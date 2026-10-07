package local

import (
	"fmt"
	"io/fs"
	"regexp"
	"strings"
)

// runtimeCompanions are the official bundles extracted over a platform's
// standard one, keyed like runtimePlatforms. The official installer adds ROCm
// for an AMD display device and a JetPack bundle by L4T release; CUDA already
// ships in the standard bundle. MLX is published, but the official installer
// never installs it, so it is not listed.
var runtimeCompanions = map[string]map[string]runtimePlatform{
	"linux/amd64": {"rocm": {"ollama-linux-amd64-rocm.tar.zst", "tar.zst", ""}},
	"linux/arm64": {
		"jetpack5": {"ollama-linux-arm64-jetpack5.tar.zst", "tar.zst", ""},
		"jetpack6": {"ollama-linux-arm64-jetpack6.tar.zst", "tar.zst", ""},
	},
}

// jetpackBundles maps an L4T major release in /etc/nv_tegra_release to its
// companion, as the official installer does: R36 is JetPack 6, R35 JetPack 5.
var jetpackBundles = map[string]string{"36": "jetpack6", "35": "jetpack5"}

var tegraRelease = regexp.MustCompile(`\bR([0-9]+)\b`)

// runtimeCompanionNeed is the companion this machine needs over the standard
// bundle, and why. Note describes accelerator hardware no official bundle
// serves; the standard bundle still runs, without that accelerator.
type runtimeCompanionNeed struct {
	Name, Reason, Note string
	// Unserved is the vendor of accelerator hardware no bundle serves here.
	Unserved string
}

// companionVendors is the accelerator vendor each companion serves, as the
// prober names it. JetPack serves an integrated Tegra GPU, which the prober
// does not list as a card, so it has none.
var companionVendors = map[string]string{"rocm": "amd"}

// detectRuntimeCompanion reads the same facts as the official installer
// through root: the L4T release file and the DRM cards' PCI vendors.
func detectRuntimeCompanion(platform string, root fs.FS) runtimeCompanionNeed {
	offered := runtimeCompanions[platform]
	if len(offered) == 0 || root == nil {
		return runtimeCompanionNeed{}
	}
	if data, err := fs.ReadFile(root, "etc/nv_tegra_release"); err == nil && offersJetpack(offered) {
		release := ""
		if match := tegraRelease.FindSubmatch(data); match != nil {
			release = string(match[1])
		}
		if name := jetpackBundles[release]; name != "" {
			if _, ok := offered[name]; ok {
				return runtimeCompanionNeed{Name: name, Reason: "NVIDIA Jetson, L4T R" + release}
			}
		}
		label := "an unrecognised L4T release"
		if release != "" {
			label = "L4T R" + release
		}
		return runtimeCompanionNeed{Note: fmt.Sprintf("this Jetson reports %s, which no official Ollama JetPack bundle supports; Ollama may not use its GPU", label)}
	}
	// The official installer stops before its ROCm step when the NVIDIA
	// driver is installed (it looks for nvidia-smi). A loaded driver shows
	// here, so a Ryzen iGPU beside an NVIDIA dGPU gets no ROCm bundle; CUDA
	// in the standard bundle serves the NVIDIA card.
	if _, err := fs.Stat(root, "proc/driver/nvidia/version"); err == nil {
		return runtimeCompanionNeed{}
	}
	for _, card := range (Prober{Root: root}).accelerators() {
		if card.Vendor != "amd" {
			continue
		}
		if _, ok := offered["rocm"]; ok {
			return runtimeCompanionNeed{Name: "rocm", Reason: "AMD GPU " + card.Name}
		}
		return runtimeCompanionNeed{Unserved: "amd", Note: fmt.Sprintf("AMD GPU %s: Ollama publishes no ROCm bundle for %s, so it runs on the CPU", card.Name, platform)}
	}
	return runtimeCompanionNeed{}
}

func offersJetpack(offered map[string]runtimePlatform) bool {
	for name := range offered {
		if strings.HasPrefix(name, "jetpack") {
			return true
		}
	}
	return false
}

// companionLabels are how people are told about each companion.
var companionLabels = map[string]string{"rocm": "ROCm", "jetpack5": "JetPack 5", "jetpack6": "JetPack 6"}

// RuntimeBundleLabel names a download for a person: the standard bundle is
// the runtime, and a companion is named for its accelerator stack.
func RuntimeBundleLabel(bundle string) string {
	if bundle == "" {
		return "Runtime"
	}
	if label, ok := companionLabels[bundle]; ok {
		return label + " bundle"
	}
	return bundle + " bundle"
}
