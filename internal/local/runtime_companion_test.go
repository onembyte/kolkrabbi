package local

import (
	"strings"
	"testing"
	"testing/fstest"
)

func drmCard(fsys fstest.MapFS, card, vendor string) {
	fsys["sys/class/drm/"+card+"/device/vendor"] = &fstest.MapFile{Data: []byte(vendor + "\n")}
}

// The official installer adds ROCm for an AMD display device and a JetPack
// bundle by /etc/nv_tegra_release; NVIDIA and everything else need only the
// standard bundle. Detection follows the same rules, per official asset.
func TestDetectRuntimeCompanionFollowsTheOfficialInstaller(t *testing.T) {
	tegra := func(line string) fstest.MapFS {
		return fstest.MapFS{"etc/nv_tegra_release": &fstest.MapFile{Data: []byte(line + "\n")}}
	}
	amd := fstest.MapFS{}
	drmCard(amd, "card0", "0x1002")
	amd["sys/class/drm/card0-DP-1/status"] = &fstest.MapFile{Data: []byte("connected")}
	nvidia := fstest.MapFS{}
	drmCard(nvidia, "card0", "0x10de")
	mixed := fstest.MapFS{}
	drmCard(mixed, "card0", "0x8086")
	drmCard(mixed, "card1", "0x1002")
	intel := fstest.MapFS{}
	drmCard(intel, "card0", "0x8086")
	// The JetPack check comes first, as in install.sh: a Jetson's own driver
	// must not hide its bundle.
	jetsonWithDriver := tegra("# R36 (release), REVISION: 4.3")
	jetsonWithDriver["proc/driver/nvidia/version"] = &fstest.MapFile{Data: []byte("NVRM version: 540.4.0\n")}
	// A Ryzen iGPU beside an NVIDIA dGPU whose driver is loaded: the official
	// installer stops at nvidia-smi before its ROCm step.
	nvidiaDriverAndAMD := fstest.MapFS{"proc/driver/nvidia/version": &fstest.MapFile{Data: []byte("NVRM version: 575.64\n")}}
	drmCard(nvidiaDriverAndAMD, "card0", "0x10de")
	drmCard(nvidiaDriverAndAMD, "card1", "0x1002")
	// The same cards without the NVIDIA driver: ROCm serves the AMD GPU.
	nvidiaCardAndAMD := fstest.MapFS{}
	drmCard(nvidiaCardAndAMD, "card0", "0x10de")
	drmCard(nvidiaCardAndAMD, "card1", "0x1002")

	cases := []struct {
		name, platform string
		root           fstest.MapFS
		want           string
		note           string
	}{
		{"amd on amd64", "linux/amd64", amd, "rocm", ""},
		{"amd beside intel", "linux/amd64", mixed, "rocm", ""},
		{"nvidia needs nothing", "linux/amd64", nvidia, "", ""},
		{"nvidia driver wins over amd", "linux/amd64", nvidiaDriverAndAMD, "", ""},
		{"amd beside an nvidia card without driver", "linux/amd64", nvidiaCardAndAMD, "rocm", ""},
		{"intel needs nothing", "linux/amd64", intel, "", ""},
		{"no cards", "linux/amd64", fstest.MapFS{}, "", ""},
		{"jetpack 6", "linux/arm64", tegra("# R36 (release), REVISION: 4.3, GCID: 38968081, BOARD: generic, EABI: aarch64"), "jetpack6", ""},
		{"jetpack 5", "linux/arm64", tegra("# R35 (release), REVISION: 5.0, GCID: 35550185, BOARD: t186ref, EABI: aarch64"), "jetpack5", ""},
		{"jetson with the nvidia driver loaded", "linux/arm64", jetsonWithDriver, "jetpack6", ""},
		{"old jetpack", "linux/arm64", tegra("# R32 (release), REVISION: 7.4"), "", "JetPack"},
		{"no digit confusion", "linux/arm64", tegra("# R350 (release)"), "", "JetPack"},
		{"amd on arm64 has no bundle", "linux/arm64", amd, "", "ROCm"},
		{"plain arm64", "linux/arm64", fstest.MapFS{}, "", ""},
		{"tegra file off arm64 is ignored", "linux/amd64", tegra("# R36 (release)"), "", ""},
		{"macOS needs nothing", "darwin/arm64", amd, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := detectRuntimeCompanion(c.platform, c.root)
			if got.Name != c.want {
				t.Fatalf("companion = %q, want %q (%+v)", got.Name, c.want, got)
			}
			if c.want != "" {
				if got.Reason == "" {
					t.Errorf("a companion without the reason it is needed: %+v", got)
				}
				if _, ok := runtimeCompanions[c.platform][got.Name]; !ok {
					t.Errorf("%s is not an official %s companion", got.Name, c.platform)
				}
			}
			if (c.note == "") != (got.Note == "") || !strings.Contains(got.Note, c.note) {
				t.Errorf("note = %q, want one naming %q", got.Note, c.note)
			}
		})
	}
}

// Only the official companions exist: MLX is published but the official
// installer never adds it, and there is no arm64 ROCm or amd64 JetPack.
func TestRuntimeCompanionsAreTheOfficialAssets(t *testing.T) {
	want := map[string]string{
		"linux/amd64/rocm":     "ollama-linux-amd64-rocm.tar.zst",
		"linux/arm64/jetpack5": "ollama-linux-arm64-jetpack5.tar.zst",
		"linux/arm64/jetpack6": "ollama-linux-arm64-jetpack6.tar.zst",
	}
	got := map[string]string{}
	for platform, companions := range runtimeCompanions {
		if _, ok := runtimePlatforms[platform]; !ok {
			t.Errorf("companions for %s, which has no standard bundle", platform)
		}
		for name, bundle := range companions {
			got[platform+"/"+name] = bundle.Asset
			if bundle.Format != runtimePlatforms[platform].Format {
				t.Errorf("%s/%s format %q differs from the standard bundle's", platform, name, bundle.Format)
			}
			if _, ok := companionLabels[name]; !ok {
				t.Errorf("%s has no label for progress and prompts", name)
			}
		}
	}
	if len(got) != len(want) {
		t.Fatalf("companions = %v, want %v", got, want)
	}
	for key, asset := range want {
		if got[key] != asset {
			t.Errorf("%s = %q, want %q", key, got[key], asset)
		}
	}
}
