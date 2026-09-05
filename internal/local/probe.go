package local

import (
	"context"
	"io/fs"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/diskspace"
	"github.com/onembyte/kolkrabbi/internal/shell"
)

// Prober fills the documented hardware shape from platform-native sources. It
// reads through an fs.FS and an injected disk-space function so the whole thing
// is testable without a GPU, without /proc, and without root.
//
// Every read fails closed to unknown. The planner refuses on unknown, so a
// probe that guesses is worse than one that admits it cannot tell.
type Prober struct {
	Root     fs.FS
	ModelDir string
	DiskFree func(path string) (uint64, bool)
	// NvidiaSMI returns the vendor tool's CSV lines. NVIDIA exposes no VRAM
	// counters in sysfs, so this is the only way to measure those cards, and it
	// is a seam because a test must never depend on a driver being installed.
	NvidiaSMI func(context.Context) ([]string, bool)
	// Sysctl answers one kernel key, the way macOS describes a machine: total
	// memory and the chip. A seam so a Mac can be described on a Linux runner
	// and vice versa. Nil, or a key it cannot answer, is unknown (V34.4d).
	Sysctl func(context.Context, string) (string, bool)
}

// NewSystemProber wires the probe to this machine: the real filesystem, a real
// statfs, and the vendor tool for cards sysfs cannot describe. Everything it
// cannot reach still degrades to unknown rather than to a number.
func NewSystemProber(modelDir string) Prober {
	return Prober{
		Root:     os.DirFS("/"),
		ModelDir: modelDir,
		DiskFree: diskspace.Free,
		NvidiaSMI: func(ctx context.Context) ([]string, bool) {
			var lines []string
			err := shell.RunLines(ctx, "nvidia-smi", []string{
				"--query-gpu=name,memory.total,memory.used",
				"--format=csv,noheader,nounits",
			}, nil, func(line []byte) error {
				lines = append(lines, string(line))
				return nil
			})
			if err != nil {
				return nil, false
			}
			return lines, true
		},
		Sysctl: func(ctx context.Context, key string) (string, bool) {
			var out []string
			err := shell.RunLines(ctx, "sysctl", []string{"-n", key}, nil, func(line []byte) error {
				out = append(out, string(line))
				return nil
			})
			if err != nil || len(out) == 0 {
				return "", false
			}
			return strings.TrimSpace(strings.Join(out, " ")), true
		},
	}
}

// cardName matches a real DRM card, not its connectors (card0-DP-1) or its
// render node (renderD128), which would otherwise be counted as accelerators.
var cardName = regexp.MustCompile(`^card[0-9]+$`)

// vendorNames covers the three vendors whose consumer GPUs can run a local
// model. An unrecognised ID is reported by its raw value rather than dropped.
var vendorNames = map[string]string{
	"0x10de": "nvidia",
	"0x1002": "amd",
	"0x8086": "intel",
}

// Probe returns one snapshot. It never fails: anything it cannot read is
// reported as unknown.
func (p Prober) Probe(ctx context.Context) Hardware {
	hardware := Hardware{
		SystemRAM: p.systemRAM(),
		DiskFree:  p.diskFree(),
	}
	hardware.Accelerators = p.fillNvidia(ctx, p.accelerators())
	// macOS has no /proc/meminfo and no sysfs: the kernel is asked directly,
	// and only where the Linux sources found nothing, so a Linux machine is
	// described exactly as before.
	if !hardware.SystemRAM.Known {
		hardware.SystemRAM = p.sysctlRAM(ctx)
	}
	if len(hardware.Accelerators) == 0 {
		hardware.Accelerators = p.appleSilicon(ctx, hardware.SystemRAM)
	}
	return hardware
}

// sysctlRAM reads hw.memsize, bytes as a bare integer. Anything else is unknown.
func (p Prober) sysctlRAM(ctx context.Context) Capacity {
	if p.Sysctl == nil {
		return Capacity{}
	}
	raw, ok := p.Sysctl(ctx, "hw.memsize")
	if !ok {
		return Capacity{}
	}
	bytes, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil || bytes == 0 {
		return Capacity{}
	}
	return Capacity{Bytes: bytes, Known: true}
}

// appleSilicon reports an Apple chip as one accelerator whose memory is the
// machine's: CPU and GPU share it, so the model that fits in RAM is the model
// that fits on the GPU, and the planner's reserved headroom is the guard. It
// is reported only when the chip name says Apple and the memory is known; an
// Intel Mac has a GPU sysctl cannot describe, and nothing is invented for it.
func (p Prober) appleSilicon(ctx context.Context, ram Capacity) []Accelerator {
	if p.Sysctl == nil || !ram.Known {
		return nil
	}
	brand, ok := p.Sysctl(ctx, "machdep.cpu.brand_string")
	if !ok || !strings.HasPrefix(strings.TrimSpace(brand), "Apple") {
		return nil
	}
	return []Accelerator{{Vendor: "apple", Name: strings.TrimSpace(brand), VRAM: ram, AvailableVRAM: ram}}
}

// fillNvidia measures NVIDIA cards, which sysfs cannot describe, using the
// vendor tool. It only does so when the number of lines matches the number of
// NVIDIA cards found: with any other count, which line describes which card is
// unknowable, and putting one card's VRAM on another would let the planner
// approve a model that cannot load. Unknown refuses; a wrong number approves.
func (p Prober) fillNvidia(ctx context.Context, cards []Accelerator) []Accelerator {
	if p.NvidiaSMI == nil {
		return cards
	}
	indices := make([]int, 0, len(cards))
	for index, card := range cards {
		if card.Vendor == "nvidia" && !card.VRAM.Known {
			indices = append(indices, index)
		}
	}
	if len(indices) == 0 {
		return cards
	}
	lines, ok := p.NvidiaSMI(ctx)
	if !ok {
		return cards
	}
	measured := parseNvidiaSMI(lines)
	if len(measured) != len(indices) {
		return cards
	}
	for position, index := range indices {
		cards[index].Name = measured[position].Name
		cards[index].VRAM = measured[position].VRAM
		cards[index].AvailableVRAM = measured[position].AvailableVRAM
	}
	return cards
}

// parseNvidiaSMI reads `--query-gpu=name,memory.total,memory.used
// --format=csv,noheader,nounits`, whose values are MiB. Anything that is not
// exactly that shape is dropped rather than guessed at, so a driver error
// printed on stdout cannot become a measurement.
func parseNvidiaSMI(lines []string) []Accelerator {
	cards := make([]Accelerator, 0, len(lines))
	for _, line := range lines {
		fields := strings.Split(line, ",")
		if len(fields) != 3 {
			continue
		}
		name := strings.TrimSpace(fields[0])
		total, totalErr := strconv.ParseUint(strings.TrimSpace(fields[1]), 10, 64)
		used, usedErr := strconv.ParseUint(strings.TrimSpace(fields[2]), 10, 64)
		if name == "" || totalErr != nil || usedErr != nil || used > total {
			continue
		}
		const mib = 1 << 20
		cards = append(cards, Accelerator{
			Vendor:        "nvidia",
			Name:          name,
			VRAM:          Capacity{Bytes: total * mib, Known: true},
			AvailableVRAM: Capacity{Bytes: (total - used) * mib, Known: true},
		})
	}
	return cards
}

func (p Prober) systemRAM() Capacity {
	line, ok := p.field("proc/meminfo", "MemTotal:")
	if !ok {
		return Capacity{}
	}
	fields := strings.Fields(line)
	// "MemTotal: 32764700 kB" — the unit is part of the contract, so a line
	// without it is not the line this parser understands.
	if len(fields) != 2 || !strings.EqualFold(fields[1], "kB") {
		return Capacity{}
	}
	kb, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return Capacity{}
	}
	return Capacity{Bytes: kb * 1024, Known: true}
}

func (p Prober) diskFree() Capacity {
	if p.DiskFree == nil {
		return Capacity{}
	}
	bytes, ok := p.DiskFree(p.ModelDir)
	if !ok {
		return Capacity{}
	}
	return Capacity{Bytes: bytes, Known: true}
}

func (p Prober) accelerators() []Accelerator {
	if p.Root == nil {
		return nil
	}
	const drm = "sys/class/drm"
	entries, err := fs.ReadDir(p.Root, drm)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if cardName.MatchString(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	cards := make([]Accelerator, 0, len(names))
	for _, name := range names {
		device := path.Join(drm, name, "device")
		vendorID, ok := p.value(path.Join(device, "vendor"))
		if !ok {
			continue
		}
		vendor := vendorNames[strings.ToLower(vendorID)]
		if vendor == "" {
			vendor = vendorID
		}
		card := Accelerator{Vendor: vendor, Name: name}
		total, totalOK := p.bytes(path.Join(device, "mem_info_vram_total"))
		used, usedOK := p.bytes(path.Join(device, "mem_info_vram_used"))
		if totalOK {
			card.VRAM = Capacity{Bytes: total, Known: true}
			if usedOK && used <= total {
				card.AvailableVRAM = Capacity{Bytes: total - used, Known: true}
			}
		}
		// A card whose counters are unreadable is still listed. The planner can
		// refuse on unknown; it cannot refuse on a card it never saw.
		cards = append(cards, card)
	}
	return cards
}

func (p Prober) field(name, prefix string) (string, bool) {
	body, ok := p.value(name)
	if !ok {
		return "", false
	}
	for _, line := range strings.Split(body, "\n") {
		if rest, found := strings.CutPrefix(line, prefix); found {
			return strings.TrimSpace(rest), true
		}
	}
	return "", false
}

func (p Prober) bytes(name string) (uint64, bool) {
	raw, ok := p.value(name)
	if !ok {
		return 0, false
	}
	value, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

func (p Prober) value(name string) (string, bool) {
	if p.Root == nil {
		return "", false
	}
	body, err := fs.ReadFile(p.Root, name)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(body)), true
}
