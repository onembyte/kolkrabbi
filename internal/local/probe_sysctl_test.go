package local

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

// The pages say /localia reads the machine's actual hardware. On a Mac the
// probe read nothing: its sources were /proc/meminfo and sysfs, so RAM was
// unknown, no accelerator was listed, and the planner refused every model as
// "system RAM is unknown". This runs on the real machine and is skipped
// elsewhere; the parsing tests below are the platform-independent proof.
func TestTheProbeReadsThisMacsMemoryAndChip(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("a Mac's own probe")
	}
	hw := NewSystemProber(t.TempDir()).Probe(context.Background())
	if !hw.SystemRAM.Known || hw.SystemRAM.Bytes == 0 {
		t.Fatalf("system RAM unknown on this Mac: %+v", hw)
	}
	if len(hw.Accelerators) == 0 {
		t.Fatalf("no accelerator reported on this Mac: %+v", hw)
	}
}

func sysctlOf(values map[string]string) func(context.Context, string) (string, bool) {
	return func(_ context.Context, key string) (string, bool) {
		v, ok := values[key]
		return v, ok
	}
}

// Apple silicon: one chip, memory shared between CPU and GPU. It is reported
// as an accelerator whose memory is the machine's, so the planner can place
// on it with its usual headroom rather than refuse on an unknown.
func TestProbeReportsAppleSiliconAsAUnifiedMemoryAccelerator(t *testing.T) {
	p := Prober{Sysctl: sysctlOf(map[string]string{
		"hw.memsize":               "17179869184",
		"machdep.cpu.brand_string": "Apple M3",
	})}
	hw := p.Probe(context.Background())
	if !hw.SystemRAM.Known || hw.SystemRAM.Bytes != 17179869184 {
		t.Fatalf("RAM = %+v, want 16 GiB known", hw.SystemRAM)
	}
	if len(hw.Accelerators) != 1 {
		t.Fatalf("accelerators = %+v, want the one chip", hw.Accelerators)
	}
	chip := hw.Accelerators[0]
	if chip.Vendor != "apple" || !strings.Contains(chip.Name, "Apple M3") {
		t.Fatalf("chip = %+v", chip)
	}
	if !chip.VRAM.Known || chip.VRAM.Bytes != 17179869184 || !chip.AvailableVRAM.Known {
		t.Fatalf("unified memory = %+v / %+v, want the machine's memory, known", chip.VRAM, chip.AvailableVRAM)
	}
}

// An Intel Mac has RAM sysctl can read and a GPU it cannot describe: RAM is
// known and no accelerator is invented.
func TestProbeReportsAnIntelMacAsRAMOnly(t *testing.T) {
	p := Prober{Sysctl: sysctlOf(map[string]string{
		"hw.memsize":               "34359738368",
		"machdep.cpu.brand_string": "Intel(R) Core(TM) i9-9980HK CPU @ 2.40GHz",
	})}
	hw := p.Probe(context.Background())
	if !hw.SystemRAM.Known || hw.SystemRAM.Bytes != 34359738368 {
		t.Fatalf("RAM = %+v", hw.SystemRAM)
	}
	if len(hw.Accelerators) != 0 {
		t.Fatalf("an accelerator was invented for an Intel Mac: %+v", hw.Accelerators)
	}
}

// A sysctl that answers nonsense, or nothing, leaves the value unknown.
func TestProbeLeavesRAMUnknownWhenSysctlIsUnusable(t *testing.T) {
	for name, values := range map[string]map[string]string{
		"garbage": {"hw.memsize": "lots"},
		"absent":  {},
	} {
		hw := Prober{Sysctl: sysctlOf(values)}.Probe(context.Background())
		if hw.SystemRAM.Known || len(hw.Accelerators) != 0 {
			t.Fatalf("%s: %+v, want everything unknown", name, hw)
		}
	}
}
