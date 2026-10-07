package local

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// CPU chosen (local.gpu_mode cpu) means no accelerator bundle: on hardware
// that would take one, setup fetches the standard bundle alone, records no
// companion, shows no GPU note, and discovery reports nothing missing. Every
// companion, ROCm and each JetPack alike.
func TestChoosingCPUSkipsEveryCompanion(t *testing.T) {
	for platform, companions := range runtimeCompanions {
		for name := range companions {
			t.Run(platform+"/"+name, func(t *testing.T) {
				f := companionInstallFixture(t, platform, name, companionMembers(name)...)
				f.installer.CPUOnly = true
				var notes []string
				f.installer.Progress = func(p RuntimeProgress) {
					if p.Stage == "note" {
						notes = append(notes, p.Note)
					}
				}
				host, err := f.installer.Ensure(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if got := strings.Join(*f.downloads, ","); got != runtimePlatforms[platform].Asset {
					t.Fatalf("downloads = %s; want the standard bundle alone", got)
				}
				if tree := installedTree(t, f.installer, host); strings.Contains(filepath.Base(tree), name) {
					t.Errorf("tree %s carries the companion CPU did not ask for", tree)
				}
				if len(notes) != 0 {
					t.Errorf("notes = %q; CPU was chosen, the GPU is not news", notes)
				}
				found, err := f.installer.Find()
				if err != nil {
					t.Fatal(err)
				}
				// Nothing missing; the card the bundle would serve is unserved
				// by this tree, a fact the fit plans need.
				if found.MissingCompanion != "" || found.UnservedVendor != companionVendors[name] {
					t.Fatalf("discovery reports %q missing, %q unserved; want nothing missing, %q unserved",
						found.MissingCompanion, found.UnservedVendor, companionVendors[name])
				}
			})
		}
	}
}

// A tree installed without the companion stays as it is once CPU is chosen:
// reuse is offline, with nothing to add.
func TestChoosingCPUReusesAStandardTree(t *testing.T) {
	f := companionInstallFixture(t, "linux/amd64", "rocm", companionMembers("rocm")...)
	amd := f.installer.Hardware
	f.installer.Hardware = nil // installed before the AMD card was there: standard only
	if _, err := f.installer.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.installer.Hardware, f.installer.CPUOnly = amd, true
	if found, err := f.installer.Find(); err != nil || found.MissingCompanion != "" {
		t.Fatalf("with CPU chosen, discovery = %+v, %v; want nothing missing", found, err)
	}
	before := f.calls.Load()
	if _, err := f.installer.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.calls.Load() != before {
		t.Fatal("with CPU chosen, reuse went to the network to add a companion")
	}
}

// CPU chosen changes what Kolk does and says, not what is true: a card the
// tree cannot serve stays unserved, so fit plans and the picker keep saying
// models run on the CPU. Only the fetch, the "missing" report and the GPU
// notes go.
func TestCPUChosenKeepsTheCardsATreeCannotServe(t *testing.T) {
	f := companionInstallFixture(t, "linux/amd64", "rocm", companionMembers("rocm")...)
	amd := f.installer.Hardware
	f.installer.Hardware = nil
	if _, err := f.installer.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.installer.Hardware, f.installer.CPUOnly = amd, true
	found, err := f.installer.Find()
	if err != nil {
		t.Fatal(err)
	}
	if found.MissingCompanion != "" || found.AcceleratorNote != "" {
		t.Fatalf("CPU chosen still reports %q missing, note %q", found.MissingCompanion, found.AcceleratorNote)
	}
	if found.UnusableVendor() != "amd" {
		t.Fatalf("UnusableVendor = %q; the tree has no ROCm, so the AMD card is not served", found.UnusableVendor())
	}
}

// The GPU notes are silent once CPU is chosen, on every kind of hardware that
// has one: a Jetson no JetPack bundle supports, an L4T release nobody
// recognises, and an AMD card on a platform with no ROCm bundle. The AMD card
// there stays unserved, a fact rather than a note.
func TestCPUChosenSilencesEveryGPUNote(t *testing.T) {
	amdOnArm := fstest.MapFS{}
	drmCard(amdOnArm, "card0", "0x1002")
	for name, c := range map[string]struct {
		hardware fstest.MapFS
		unserved string
	}{
		"jetson r32":       {fstest.MapFS{"etc/nv_tegra_release": &fstest.MapFile{Data: []byte("# R32 (release), REVISION: 7.1\n")}}, ""},
		"unrecognised l4t": {fstest.MapFS{"etc/nv_tegra_release": &fstest.MapFile{Data: []byte("a line nobody wrote\n")}}, ""},
		"amd without rocm": {amdOnArm, "amd"},
	} {
		t.Run(name, func(t *testing.T) {
			for _, cpu := range []bool{false, true} {
				i, _, _ := runtimeInstallFixture(t, "linux/arm64")
				i.Hardware, i.CPUOnly = c.hardware, cpu
				var notes []string
				i.Progress = func(p RuntimeProgress) {
					if p.Stage == "note" {
						notes = append(notes, p.Note)
					}
				}
				if _, err := i.Ensure(context.Background()); err != nil {
					t.Fatal(err)
				}
				found, err := i.Find()
				if err != nil {
					t.Fatal(err)
				}
				noted := len(notes) > 0 || found.AcceleratorNote != ""
				if noted == cpu {
					t.Errorf("cpu %v: notes %q, AcceleratorNote %q", cpu, notes, found.AcceleratorNote)
				}
				if found.UnservedVendor != c.unserved {
					t.Errorf("cpu %v: UnservedVendor %q, want %q", cpu, found.UnservedVendor, c.unserved)
				}
			}
		})
	}
}

// A running runtime reports the choice in force now: its facts are read again
// from the tree it runs, so a /config change mid-session shows at once, in
// both directions. A tree setup replaced since keeps the facts it started with.
func TestARunningRuntimeReportsTheChoiceInForce(t *testing.T) {
	cpu, binary := false, "/opt/ollama"
	f := newStarterFixture(0, 43111)
	f.starter.Discover = func(context.Context) Host {
		host := Host{State: HostInstalled, Managed: true, Binary: binary}
		if cpu {
			host.UnservedVendor = "amd"
		} else {
			host.MissingCompanion, host.CompanionVendor = "ROCm bundle for AMD GPU card0", "amd"
			host.CompanionFailure = "native runtime SHA-256 verification failed"
		}
		return host
	}
	if _, err := f.starter.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer f.starter.Close()
	if got := f.starter.Host(context.Background()); got.MissingCompanion == "" {
		t.Fatalf("started under auto: %+v, want ROCm missing", got)
	}
	cpu = true
	if got := f.starter.Host(context.Background()); got.MissingCompanion != "" || got.CompanionFailure != "" || got.UnusableVendor() != "amd" {
		t.Fatalf("after CPU was chosen: %+v, want nothing missing, no failure, and AMD unserved", got)
	}
	cpu = false
	if got := f.starter.Host(context.Background()); got.MissingCompanion == "" || got.UnusableVendor() != "amd" {
		t.Fatalf("back to auto: %+v, want ROCm missing again", got)
	}
	cpu, binary = true, "/opt/a-newer-tree/ollama"
	if got := f.starter.Host(context.Background()); got.MissingCompanion == "" {
		t.Fatalf("a replaced tree's facts reached the running runtime: %+v", got)
	}
}
