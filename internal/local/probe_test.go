package local

import (
	"context"
	"testing"
	"testing/fstest"
)

func TestProbeReadsSystemRAMFromMeminfo(t *testing.T) {
	prober := Prober{Root: fstest.MapFS{
		"proc/meminfo": &fstest.MapFile{Data: []byte("MemTotal:       32764700 kB\nMemFree:  100 kB\n")},
	}}

	hardware := prober.Probe(context.Background())
	if !hardware.SystemRAM.Known {
		t.Fatal("MemTotal was present and was not read")
	}
	if want := uint64(32764700) * 1024; hardware.SystemRAM.Bytes != want {
		t.Fatalf("system RAM = %d, want %d", hardware.SystemRAM.Bytes, want)
	}
}

func TestProbeLeavesRAMUnknownWhenItCannotBeRead(t *testing.T) {
	for name, root := range map[string]fstest.MapFS{
		"absent":    {},
		"malformed": {"proc/meminfo": &fstest.MapFile{Data: []byte("MemTotal: not-a-number kB\n")}},
		"no units":  {"proc/meminfo": &fstest.MapFile{Data: []byte("MemTotal: 32764700\n")}},
	} {
		hardware := Prober{Root: root}.Probe(context.Background())
		// Unknown must never arrive as a confident zero: the planner refuses on
		// unknown and would happily approve a zero-RAM machine's "fit".
		if hardware.SystemRAM.Known {
			t.Fatalf("%s meminfo was reported as a known value", name)
		}
	}
}

func TestProbeReadsAmdVramFromSysfs(t *testing.T) {
	prober := Prober{Root: fstest.MapFS{
		"sys/class/drm/card0/device/vendor":              &fstest.MapFile{Data: []byte("0x1002\n")},
		"sys/class/drm/card0/device/mem_info_vram_total": &fstest.MapFile{Data: []byte("17163091968\n")},
		"sys/class/drm/card0/device/mem_info_vram_used":  &fstest.MapFile{Data: []byte("1163091968\n")},
	}}

	hardware := prober.Probe(context.Background())
	if len(hardware.Accelerators) != 1 {
		t.Fatalf("accelerators = %+v, want one", hardware.Accelerators)
	}
	card := hardware.Accelerators[0]
	if card.Vendor != "amd" {
		t.Fatalf("vendor = %q", card.Vendor)
	}
	if !card.VRAM.Known || card.VRAM.Bytes != 17163091968 {
		t.Fatalf("vram = %+v", card.VRAM)
	}
	if !card.AvailableVRAM.Known || card.AvailableVRAM.Bytes != 17163091968-1163091968 {
		t.Fatalf("available vram = %+v, want total minus used", card.AvailableVRAM)
	}
}

func TestProbeListsACardWhoseVramItCannotRead(t *testing.T) {
	// An NVIDIA card exposes no VRAM counters in sysfs. Hiding the card would
	// be worse than listing it as unmeasured: the planner can refuse on
	// unknown, but it cannot refuse on absent.
	hardware := Prober{Root: fstest.MapFS{
		"sys/class/drm/card0/device/vendor": &fstest.MapFile{Data: []byte("0x10de\n")},
	}}.Probe(context.Background())

	if len(hardware.Accelerators) != 1 {
		t.Fatalf("accelerators = %+v, want the card listed", hardware.Accelerators)
	}
	card := hardware.Accelerators[0]
	if card.Vendor != "nvidia" {
		t.Fatalf("vendor = %q", card.Vendor)
	}
	if card.VRAM.Known || card.AvailableVRAM.Known {
		t.Fatalf("unreadable VRAM was reported as known: %+v", card)
	}
}

func TestProbeIgnoresConnectorsAndRenderNodes(t *testing.T) {
	hardware := Prober{Root: fstest.MapFS{
		"sys/class/drm/card0/device/vendor":      &fstest.MapFile{Data: []byte("0x1002\n")},
		"sys/class/drm/card0-DP-1/device/vendor": &fstest.MapFile{Data: []byte("0x1002\n")},
		"sys/class/drm/renderD128/device/vendor": &fstest.MapFile{Data: []byte("0x1002\n")},
	}}.Probe(context.Background())

	if len(hardware.Accelerators) != 1 {
		t.Fatalf("accelerators = %+v, want only the card itself", hardware.Accelerators)
	}
}

func TestProbeReportsDiskFreeThroughTheInjectedSeam(t *testing.T) {
	prober := Prober{
		Root:     fstest.MapFS{},
		ModelDir: "/var/lib/kolk/models",
		DiskFree: func(path string) (uint64, bool) {
			if path != "/var/lib/kolk/models" {
				t.Fatalf("disk was measured at %q, want the managed model directory", path)
			}
			return 200 << 30, true
		},
	}

	hardware := prober.Probe(context.Background())
	if !hardware.DiskFree.Known || hardware.DiskFree.Bytes != 200<<30 {
		t.Fatalf("disk free = %+v", hardware.DiskFree)
	}
}

func TestProbeLeavesDiskUnknownWithoutASeam(t *testing.T) {
	if hardware := (Prober{Root: fstest.MapFS{}}).Probe(context.Background()); hardware.DiskFree.Known {
		t.Fatal("disk free was claimed without any way to measure it")
	}
}

func TestProbeFillsNvidiaVramFromTheVendorTool(t *testing.T) {
	prober := Prober{
		Root: fstest.MapFS{
			"sys/class/drm/card0/device/vendor": &fstest.MapFile{Data: []byte("0x10de\n")},
		},
		NvidiaSMI: func(context.Context) ([]string, bool) {
			// name, memory.total, memory.used in MiB
			return []string{"NVIDIA GeForce RTX 4090, 24564, 1024"}, true
		},
	}

	hardware := prober.Probe(context.Background())
	if len(hardware.Accelerators) != 1 {
		t.Fatalf("accelerators = %+v", hardware.Accelerators)
	}
	card := hardware.Accelerators[0]
	if card.Name != "NVIDIA GeForce RTX 4090" {
		t.Fatalf("name = %q, want the vendor tool's name", card.Name)
	}
	if !card.VRAM.Known || card.VRAM.Bytes != 24564*1024*1024 {
		t.Fatalf("vram = %+v", card.VRAM)
	}
	if !card.AvailableVRAM.Known || card.AvailableVRAM.Bytes != (24564-1024)*1024*1024 {
		t.Fatalf("available = %+v, want total minus used", card.AvailableVRAM)
	}
}

func TestProbeLeavesNvidiaUnknownWhenTheCountsDoNotLineUp(t *testing.T) {
	// Two cards in sysfs and one line from the vendor tool: which card the line
	// describes is unknowable, and guessing would put one card's VRAM on
	// another. Unknown refuses; a wrong number approves.
	prober := Prober{
		Root: fstest.MapFS{
			"sys/class/drm/card0/device/vendor": &fstest.MapFile{Data: []byte("0x10de\n")},
			"sys/class/drm/card1/device/vendor": &fstest.MapFile{Data: []byte("0x10de\n")},
		},
		NvidiaSMI: func(context.Context) ([]string, bool) {
			return []string{"NVIDIA GeForce RTX 4090, 24564, 1024"}, true
		},
	}

	for _, card := range prober.Probe(context.Background()).Accelerators {
		if card.VRAM.Known {
			t.Fatalf("card %+v was given a number that may belong to another card", card)
		}
	}
}

func TestProbeIgnoresUnusableVendorToolOutput(t *testing.T) {
	prober := Prober{
		Root: fstest.MapFS{
			"sys/class/drm/card0/device/vendor": &fstest.MapFile{Data: []byte("0x10de\n")},
		},
		NvidiaSMI: func(context.Context) ([]string, bool) {
			return []string{"Failed to initialize NVML: Driver/library version mismatch"}, true
		},
	}

	card := prober.Probe(context.Background()).Accelerators[0]
	if card.VRAM.Known {
		t.Fatalf("an error line was parsed as a measurement: %+v", card)
	}
	if card.Name != "card0" {
		t.Fatalf("name = %q, want the sysfs name kept", card.Name)
	}
}

func TestProbeDoesNotTouchNonNvidiaCardsWithTheVendorTool(t *testing.T) {
	prober := Prober{
		Root: fstest.MapFS{
			"sys/class/drm/card0/device/vendor":              &fstest.MapFile{Data: []byte("0x1002\n")},
			"sys/class/drm/card0/device/mem_info_vram_total": &fstest.MapFile{Data: []byte("17163091968\n")},
		},
		NvidiaSMI: func(context.Context) ([]string, bool) {
			t.Fatal("nvidia-smi must not be consulted when no NVIDIA card is present")
			return nil, false
		},
	}
	prober.Probe(context.Background())
}

func TestNewSystemProberWiresEverySeam(t *testing.T) {
	prober := NewSystemProber("/var/lib/kolk/models")
	if prober.Root == nil || prober.DiskFree == nil || prober.NvidiaSMI == nil {
		t.Fatalf("system prober left a seam unwired: %+v", prober)
	}
	if prober.ModelDir != "/var/lib/kolk/models" {
		t.Fatalf("model dir = %q", prober.ModelDir)
	}
	// It must produce a snapshot on this machine without a GPU, a driver, or
	// any privileged access.
	hardware := prober.Probe(context.Background())
	if !hardware.DiskFree.Known && !hardware.SystemRAM.Known {
		t.Skip("neither disk nor RAM is measurable on this platform")
	}
}
