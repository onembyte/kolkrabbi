package local

import (
	"fmt"
	"strings"
)

// Capacity is a byte count that may be unknown. A probe that cannot read a
// value must say so rather than report zero: zero is a fact about the machine,
// and "unknown" must never be mistaken for one.
type Capacity struct {
	Bytes uint64
	Known bool
}

// Accelerator is one GPU as the planner sees it, in the shape every probe
// adapter must return regardless of how it learned the numbers.
type Accelerator struct {
	Vendor        string
	Name          string
	VRAM          Capacity
	AvailableVRAM Capacity
}

// Hardware is the deterministic, probe-independent snapshot the planner reads.
type Hardware struct {
	Accelerators []Accelerator
	SystemRAM    Capacity
	DiskFree     Capacity
}

// Config is the persisted, non-secret local-model configuration.
type Config struct {
	GPUMode              string // auto | cpu | gpu
	GPUIndex             int
	Quantization         string
	ReservedVRAMFraction float64
	ReservedRAMBytes     uint64
}

// ModelRequirement is what one model variant needs, separated into what it
// occupies on disk and what it needs at runtime. File size alone never proves
// a model fits.
type ModelRequirement struct {
	Name         string
	StorageBytes uint64
	VRAMBytes    uint64
	RAMBytes     uint64
}

// Placement is where a model would run.
type Placement string

const (
	PlacementGPU Placement = "gpu"
	PlacementCPU Placement = "cpu"
)

// FitPlan is what the user confirms before anything is downloaded. Every number
// the decision rested on is part of it.
type FitPlan struct {
	Model          string
	Placement      Placement
	Accelerator    string
	GPUIndex       int
	StorageBytes   uint64
	RequiredBytes  uint64
	ReservedBytes  uint64
	AvailableBytes uint64
	DiskFreeBytes  uint64
	// Fallback records that the preferred placement was not available, so a
	// slower run is never a surprise discovered at inference time.
	Fallback string
}

// PlanFit decides where a model would run, or refuses with the numbers that
// made it refuse. It never reports a fit it is not sure of: an unknown capacity
// is a refusal, not an optimistic guess.
func PlanFit(hardware Hardware, config Config, model ModelRequirement) (FitPlan, error) {
	if !hardware.DiskFree.Known {
		return FitPlan{}, fmt.Errorf("free disk space is unknown, so %s cannot be pulled safely", model.Name)
	}
	if model.StorageBytes > hardware.DiskFree.Bytes {
		return FitPlan{}, fmt.Errorf("%s needs %s on disk and only %s is free",
			model.Name, humanBytes(model.StorageBytes), humanBytes(hardware.DiskFree.Bytes))
	}

	mode := strings.ToLower(strings.TrimSpace(config.GPUMode))
	if mode == "" {
		mode = "auto"
	}
	plan := FitPlan{
		Model: model.Name, StorageBytes: model.StorageBytes,
		DiskFreeBytes: hardware.DiskFree.Bytes, GPUIndex: -1,
	}

	switch mode {
	case "cpu":
		return planOnCPU(plan, hardware, config, model, "")
	case "gpu":
		index := config.GPUIndex
		if index < 0 || index >= len(hardware.Accelerators) {
			return FitPlan{}, fmt.Errorf("gpu %d was selected but %d accelerators were detected; choose another index or set gpu mode to cpu",
				index, len(hardware.Accelerators))
		}
		fitted, err := planOnGPU(plan, hardware.Accelerators[index], index, config, model)
		if err != nil {
			return FitPlan{}, fmt.Errorf("%w; set gpu mode to cpu, or choose a smaller quantization", err)
		}
		return fitted, nil
	case "auto":
		// The largest single fitting card, never several pooled together: that
		// is an explicit opt-in, not a default.
		best, bestIndex, bestFree := Accelerator{}, -1, uint64(0)
		for index, accelerator := range hardware.Accelerators {
			usable, ok := usableVRAM(accelerator, config)
			if !ok || usable < model.VRAMBytes {
				continue
			}
			if bestIndex < 0 || usable > bestFree {
				best, bestIndex, bestFree = accelerator, index, usable
			}
		}
		if bestIndex >= 0 {
			return planOnGPU(plan, best, bestIndex, config, model)
		}
		return planOnCPU(plan, hardware, config, model, "no accelerator could hold the model, so it would run on the cpu")
	default:
		return FitPlan{}, fmt.Errorf("gpu mode %q is not one of auto, cpu or gpu", config.GPUMode)
	}
}

func planOnGPU(plan FitPlan, accelerator Accelerator, index int, config Config, model ModelRequirement) (FitPlan, error) {
	usable, ok := usableVRAM(accelerator, config)
	if !ok {
		return FitPlan{}, fmt.Errorf("available VRAM on %s is unknown, so %s cannot be placed on it",
			accelerator.Name, model.Name)
	}
	if usable < model.VRAMBytes {
		return FitPlan{}, fmt.Errorf("%s needs %s of VRAM and %s offers %s after reserved headroom",
			model.Name, humanBytes(model.VRAMBytes), accelerator.Name, humanBytes(usable))
	}
	plan.Placement = PlacementGPU
	plan.Accelerator = accelerator.Name
	plan.GPUIndex = index
	plan.RequiredBytes = model.VRAMBytes
	plan.AvailableBytes = usable
	plan.ReservedBytes = accelerator.AvailableVRAM.Bytes - usable
	return plan, nil
}

func planOnCPU(plan FitPlan, hardware Hardware, config Config, model ModelRequirement, fallback string) (FitPlan, error) {
	if !hardware.SystemRAM.Known {
		return FitPlan{}, fmt.Errorf("system RAM is unknown, so %s cannot be pulled safely", model.Name)
	}
	usable := uint64(0)
	if hardware.SystemRAM.Bytes > config.ReservedRAMBytes {
		usable = hardware.SystemRAM.Bytes - config.ReservedRAMBytes
	}
	if usable < model.RAMBytes {
		// Swap would make this "work" and then be unusably slow, which is a
		// worse answer than no.
		return FitPlan{}, fmt.Errorf("%s needs %s of RAM and only %s is available after reserved headroom",
			model.Name, humanBytes(model.RAMBytes), humanBytes(usable))
	}
	plan.Placement = PlacementCPU
	plan.RequiredBytes = model.RAMBytes
	plan.AvailableBytes = usable
	plan.ReservedBytes = config.ReservedRAMBytes
	plan.Fallback = fallback
	return plan, nil
}

func usableVRAM(accelerator Accelerator, config Config) (uint64, bool) {
	if !accelerator.AvailableVRAM.Known {
		return 0, false
	}
	fraction := config.ReservedVRAMFraction
	if fraction < 0 || fraction >= 1 {
		fraction = 0
	}
	reserved := uint64(float64(accelerator.AvailableVRAM.Bytes) * fraction)
	return accelerator.AvailableVRAM.Bytes - reserved, true
}

// HumanBytes formats a byte count for a person reading a plan or a status
// line. Sizes here decide whether a multi-gigabyte download is worth starting.
func HumanBytes(b uint64) string { return humanBytes(b) }

func humanBytes(b uint64) string {
	const unit = 1 << 10
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	value, exponent := float64(b)/unit, 0
	for value >= unit && exponent < 3 {
		value /= unit
		exponent++
	}
	return fmt.Sprintf("%.1f %ciB", value, "KMGT"[exponent])
}
