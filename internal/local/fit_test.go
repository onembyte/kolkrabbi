package local

import (
	"strings"
	"testing"
)

const gib = 1 << 30

func gpu(name string, vram, available uint64) Accelerator {
	return Accelerator{
		Vendor: "nvidia", Name: name,
		VRAM:          Capacity{Bytes: vram, Known: true},
		AvailableVRAM: Capacity{Bytes: available, Known: true},
	}
}

func machine(accelerators ...Accelerator) Hardware {
	return Hardware{
		Accelerators: accelerators,
		SystemRAM:    Capacity{Bytes: 32 * gib, Known: true},
		DiskFree:     Capacity{Bytes: 200 * gib, Known: true},
	}
}

func model(storage, vram, ram uint64) ModelRequirement {
	return ModelRequirement{Name: "test/model", StorageBytes: storage, VRAMBytes: vram, RAMBytes: ram}
}

func TestPlanFitPrefersAGPUThatFitsAfterHeadroom(t *testing.T) {
	plan, err := PlanFit(machine(gpu("RTX 4090", 24*gib, 22*gib)), Config{GPUMode: "auto", ReservedVRAMFraction: 0.1},
		model(12*gib, 14*gib, 20*gib))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Placement != PlacementGPU || plan.GPUIndex != 0 {
		t.Fatalf("plan = %+v, want the GPU", plan)
	}
	// 22 GiB available, 10% reserved -> 19.8 usable, model needs 14.
	if plan.ReservedBytes == 0 || plan.AvailableBytes == 0 {
		t.Fatalf("plan must show the headroom it reserved: %+v", plan)
	}
}

func TestPlanFitFallsBackToTheCPUAndSaysSo(t *testing.T) {
	plan, err := PlanFit(machine(gpu("GTX 1060", 6*gib, 6*gib)), Config{GPUMode: "auto"},
		model(12*gib, 14*gib, 20*gib))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Placement != PlacementCPU {
		t.Fatalf("plan = %+v, want the CPU", plan)
	}
	if plan.Fallback == "" {
		t.Fatal("a fallback placement must be reported, not discovered later")
	}
}

func TestPlanFitRefusesRatherThanSwapping(t *testing.T) {
	// Needs more RAM than the machine has once headroom is reserved.
	_, err := PlanFit(machine(), Config{GPUMode: "cpu", ReservedRAMBytes: 4 * gib}, model(12*gib, 0, 30*gib))
	if err == nil {
		t.Fatal("a model that does not fit in RAM must be refused, never swapped")
	}
	if !strings.Contains(err.Error(), "GiB") {
		t.Fatalf("error = %v, want the numbers that made it refuse", err)
	}
}

func TestPlanFitRefusesAnExplicitGPUThatCannotHoldTheModel(t *testing.T) {
	// gpu mode is a choice, not a hint: silently using the CPU would make a
	// deliberate GPU run quietly slow instead of honestly refused.
	_, err := PlanFit(machine(gpu("GTX 1060", 6*gib, 6*gib)), Config{GPUMode: "gpu"}, model(12*gib, 14*gib, 20*gib))
	if err == nil {
		t.Fatal("an explicit gpu mode that does not fit must be refused")
	}
	if !strings.Contains(err.Error(), "cpu") {
		t.Fatalf("error = %v, want it to name the way out", err)
	}
}

func TestPlanFitRefusesWhenCapacityIsUnknown(t *testing.T) {
	unknown := Hardware{SystemRAM: Capacity{}, DiskFree: Capacity{Bytes: 200 * gib, Known: true}}
	_, err := PlanFit(unknown, Config{GPUMode: "cpu"}, model(12*gib, 0, 8*gib))
	if err == nil {
		t.Fatal("unknown capacity must never authorize a pull")
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("error = %v, want the unknown capacity named", err)
	}
}

func TestPlanFitRefusesWhenTheModelDoesNotFitOnDisk(t *testing.T) {
	small := machine()
	small.DiskFree = Capacity{Bytes: 8 * gib, Known: true}
	_, err := PlanFit(small, Config{GPUMode: "cpu"}, model(12*gib, 0, 8*gib))
	if err == nil {
		t.Fatal("a model larger than free disk must be refused before anything is downloaded")
	}
}

func TestPlanFitHonoursAnExplicitGPUIndex(t *testing.T) {
	plan, err := PlanFit(
		machine(gpu("GTX 1060", 6*gib, 6*gib), gpu("RTX 4090", 24*gib, 24*gib)),
		Config{GPUMode: "gpu", GPUIndex: 1}, model(12*gib, 14*gib, 20*gib))
	if err != nil {
		t.Fatal(err)
	}
	if plan.GPUIndex != 1 || plan.Accelerator != "RTX 4090" {
		t.Fatalf("plan = %+v, want the GPU the user chose", plan)
	}
}

func TestPlanFitDoesNotSpreadAcrossGPUs(t *testing.T) {
	// Two cards that only fit the model together must not be treated as one.
	_, err := PlanFit(
		machine(gpu("A", 8*gib, 8*gib), gpu("B", 8*gib, 8*gib)),
		Config{GPUMode: "gpu"}, model(12*gib, 14*gib, 64*gib))
	if err == nil {
		t.Fatal("two cards must not be pooled without the user opting in")
	}
}
