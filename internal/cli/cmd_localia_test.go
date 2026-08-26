package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/local"
)

func fakeHardware() local.Hardware {
	const gib = 1 << 30
	return local.Hardware{
		Accelerators: []local.Accelerator{{
			Vendor: "amd", Name: "card0",
			VRAM:          local.Capacity{Bytes: 16 * gib, Known: true},
			AvailableVRAM: local.Capacity{Bytes: 15 * gib, Known: true},
		}, {
			Vendor: "nvidia", Name: "card1",
		}},
		SystemRAM: local.Capacity{Bytes: 32 * gib, Known: true},
		DiskFree:  local.Capacity{Bytes: 200 * gib, Known: true},
	}
}

func TestLocaliaReportsHardwareAndStorage(t *testing.T) {
	isolateConnectorState(t)
	a, out, errOut := newTestApp("")
	a.probeHardware = func(context.Context, string) local.Hardware { return fakeHardware() }

	if code := a.main(context.Background(), []string{"localia"}); code != ExitOK {
		t.Fatalf("localia exit = %d, stderr = %q", code, errOut.String())
	}

	got := out.String()
	for _, want := range []string{"32.0 GiB", "200.0 GiB", "card0", "card1", "amd", "nvidia"} {
		if !strings.Contains(got, want) {
			t.Fatalf("localia output = %q, want %q", got, want)
		}
	}
	// A card Kolkrabbi could not measure must say so rather than read as 0 B.
	if !strings.Contains(got, "unknown") {
		t.Fatalf("localia output = %q, want the unmeasured card marked unknown", got)
	}
}

func TestLocaliaNamesTheDirectoryItManages(t *testing.T) {
	dirs := isolateConnectorState(t)
	a, out, _ := newTestApp("")
	a.probeHardware = func(context.Context, string) local.Hardware { return fakeHardware() }

	if code := a.main(context.Background(), []string{"localia"}); code != ExitOK {
		t.Fatal("localia must succeed")
	}
	if !strings.Contains(out.String(), dirs.LocalModelsDir()) {
		t.Fatalf("localia output = %q, want the managed model directory named", out.String())
	}
}

func TestLocaliaSaysNoModelIsInstalledYet(t *testing.T) {
	isolateConnectorState(t)
	a, out, _ := newTestApp("")
	a.probeHardware = func(context.Context, string) local.Hardware { return fakeHardware() }

	if code := a.main(context.Background(), []string{"localia"}); code != ExitOK {
		t.Fatal("localia must succeed with no models installed")
	}
	if !strings.Contains(out.String(), "no local model") {
		t.Fatalf("localia output = %q, want it to say nothing is installed", out.String())
	}
}

func TestSlashLocaliaMirrorsTheCommand(t *testing.T) {
	isolateConnectorState(t)
	a, ag, out := replFixture(t, "")
	a.probeHardware = func(context.Context, string) local.Hardware { return fakeHardware() }

	if a.slash(context.Background(), ag, "/localia") {
		t.Fatal("/localia must not exit the session")
	}
	if !strings.Contains(out.String(), "32.0 GiB") {
		t.Fatalf("slash localia output = %q", out.String())
	}
}

func TestLocaliaNeedsNoGpuOrOllama(t *testing.T) {
	// The default probe must run on a machine with neither, and still print a
	// usable report rather than failing.
	isolateConnectorState(t)
	a, out, errOut := newTestApp("")

	if code := a.main(context.Background(), []string{"localia"}); code != ExitOK {
		t.Fatalf("localia exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "system RAM") {
		t.Fatalf("localia output = %q", out.String())
	}
}
