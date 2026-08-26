package local

import (
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
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
func (p Prober) Probe() Hardware {
	hardware := Hardware{
		SystemRAM: p.systemRAM(),
		DiskFree:  p.diskFree(),
	}
	hardware.Accelerators = p.accelerators()
	return hardware
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
