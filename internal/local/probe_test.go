package local

import (
	"testing"
	"testing/fstest"
)

func TestProbeReadsSystemRAMFromMeminfo(t *testing.T) {
	prober := Prober{Root: fstest.MapFS{
		"proc/meminfo": &fstest.MapFile{Data: []byte("MemTotal:       32764700 kB\nMemFree:  100 kB\n")},
	}}

	hardware := prober.Probe()
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
		hardware := Prober{Root: root}.Probe()
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

	hardware := prober.Probe()
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
	}}.Probe()

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
	}}.Probe()

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

	hardware := prober.Probe()
	if !hardware.DiskFree.Known || hardware.DiskFree.Bytes != 200<<30 {
		t.Fatalf("disk free = %+v", hardware.DiskFree)
	}
}

func TestProbeLeavesDiskUnknownWithoutASeam(t *testing.T) {
	if hardware := (Prober{Root: fstest.MapFS{}}).Probe(); hardware.DiskFree.Known {
		t.Fatal("disk free was claimed without any way to measure it")
	}
}
