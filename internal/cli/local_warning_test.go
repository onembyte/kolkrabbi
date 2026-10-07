package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/local"
	"github.com/onembyte/kolkrabbi/internal/tui"
)

func TestTUIStatusCarriesLiveLocalPlacementWarning(t *testing.T) {
	ctx := context.Background()
	a, ag, _ := replFixture(t, "")
	ag.SetSessionModel("ollama/qwen2.5-coder:7b")
	a.probeHardware = func(context.Context, string) local.Hardware {
		return local.Hardware{SystemRAM: local.Capacity{Bytes: 32 << 30, Known: true}}
	}
	if got := a.tuiLocalStatus(ctx, ag, "ready", "~/project").LocalWarning; got == "" {
		t.Fatal("TUI status lost the local CPU warning")
	}
	if err := a.runConfig(ctx, []string{"set", "local.gpu_mode", "cpu"}); err != nil {
		t.Fatal(err)
	}
	if got := a.tuiLocalStatus(ctx, ag, "ready", "~/project").LocalWarning; got != "" {
		t.Fatalf("TUI status kept warning after the persisted CPU choice: %q", got)
	}
}

func TestLocalPlacementWarningFollowsTheChoiceAndActualMachine(t *testing.T) {
	ctx := context.Background()
	a, _, _ := newTestApp(t, "")
	a.probeHardware = func(context.Context, string) local.Hardware { return fakeHardware() }
	a.discoverHost = func(context.Context) local.Host {
		return local.Host{State: local.HostInstalled, Binary: "/fixture/ollama"}
	}
	model := "ollama/qwen2.5-coder:7b"
	if got := a.localPlacementWarning(ctx, model); got != "" {
		t.Fatalf("a fitting GPU produced a warning: %q", got)
	}
	a.discoverHost = func(context.Context) local.Host {
		return local.Host{State: local.HostInstalled, Binary: "/fixture/ollama", UnservedVendor: "amd"}
	}
	if got := a.localPlacementWarning(ctx, model); !strings.Contains(got, "CPU") || !strings.Contains(got, "/config set local.gpu_mode cpu") {
		t.Fatalf("unusable GPU warning = %q", got)
	}
	for _, model := range []string{"remote/model", "ollama/gpt-oss:120b-cloud"} {
		if got := a.localPlacementWarning(ctx, model); got != "" {
			t.Errorf("remote model %s warned about the local CPU: %q", model, got)
		}
	}
	if err := a.runConfig(ctx, []string{"set", "local.gpu_mode", "cpu"}); err != nil {
		t.Fatal(err)
	}
	if got := a.localPlacementWarning(ctx, "ollama/qwen2.5-coder:7b"); got != "" {
		t.Fatalf("explicit CPU choice did not dismiss warning: %q", got)
	}
	d, err := a.locate()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(d.ConfigFile())
	if err != nil || cfg.Local.GPUMode != "cpu" {
		t.Fatalf("CPU choice was not durable: %+v, %v", cfg, err)
	}
	if err := a.runConfig(ctx, []string{"set", "local.gpu_mode", "auto"}); err != nil {
		t.Fatal(err)
	}
	if got := a.localPlacementWarning(ctx, "ollama/qwen2.5-coder:7b"); got == "" {
		t.Fatal("switching back to auto did not restore the warning")
	}
	if err := a.runConfig(ctx, []string{"set", "local.gpu_mode", "gpu"}); err != nil {
		t.Fatal(err)
	}
	if got := a.localPlacementWarning(ctx, "ollama/qwen2.5-coder:7b"); got == "" {
		t.Fatal("a GPU setting dismissed the warning without an explicit CPU choice")
	}
}

func TestCanceledTurnDoesNotInventCPUPlacement(t *testing.T) {
	a, _, _ := newTestApp(t, "")
	a.probeHardware = func(ctx context.Context, _ string) local.Hardware {
		if ctx.Err() != nil {
			return local.Hardware{}
		}
		return fakeHardware()
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := a.localPlacementWarning(ctx, "ollama/qwen2.5-coder:7b"); got != "" {
		t.Fatalf("canceling a turn fabricated CPU placement: %q", got)
	}
}

func TestWarningJudgesAutomaticPlacementEvenWithAnExplicitGPUIndex(t *testing.T) {
	a, _, _ := newTestApp(t, "")
	a.probeHardware = func(context.Context, string) local.Hardware {
		return local.Hardware{
			Accelerators: []local.Accelerator{
				{Vendor: "nvidia", Name: "small", AvailableVRAM: local.Capacity{Bytes: 1 << 30, Known: true}},
				{Vendor: "nvidia", Name: "large", AvailableVRAM: local.Capacity{Bytes: 16 << 30, Known: true}},
			},
			SystemRAM: local.Capacity{Bytes: 32 << 30, Known: true},
		}
	}
	ctx := context.Background()
	if err := a.runConfig(ctx, []string{"set", "local.gpu_index", "0"}); err != nil {
		t.Fatal(err)
	}
	if err := a.runConfig(ctx, []string{"set", "local.gpu_mode", "gpu"}); err != nil {
		t.Fatal(err)
	}
	if got := a.localPlacementWarning(ctx, "ollama/qwen2.5-coder:7b"); got != "" {
		t.Fatalf("auto would fit on the second GPU, yet footer warns about CPU: %q", got)
	}
}

func TestAutomaticCPUWarningCoversNoGPUAndTooSmallGPU(t *testing.T) {
	ctx := context.Background()
	for _, cards := range [][]local.Accelerator{
		nil,
		{{Vendor: "nvidia", Name: "small", AvailableVRAM: local.Capacity{Bytes: 1 << 30, Known: true}}},
	} {
		a, _, _ := newTestApp(t, "")
		a.probeHardware = func(context.Context, string) local.Hardware {
			return local.Hardware{Accelerators: cards, SystemRAM: local.Capacity{Bytes: 32 << 30, Known: true}, DiskFree: local.Capacity{Bytes: 50 << 30, Known: true}}
		}
		if got := a.localPlacementWarning(ctx, "ollama/qwen2.5-coder:7b"); got == "" {
			t.Fatalf("auto placed local model on CPU with %d cards, but no persistent warning", len(cards))
		}
		got := a.localPlacementWarning(ctx, "ollama/custom:latest")
		if len(cards) == 0 && !strings.Contains(got, "CPU fallback") {
			t.Fatalf("unknown-size custom model with no GPU lacks definite CPU warning: %q", got)
		}
		if len(cards) > 0 && (!strings.Contains(got, "CPU possible") || !strings.Contains(got, "/config set local.gpu_mode cpu")) {
			t.Fatalf("unknown-size custom model with a usable GPU lacks uncertainty warning: %q", got)
		}
	}
}

func TestNarrowTUIShowsCPUInActualPlacementWarnings(t *testing.T) {
	for _, tc := range []struct {
		name  string
		model string
		cards []local.Accelerator
	}{
		{name: "definite", model: "ollama/qwen2.5-coder:7b"},
		{name: "uncertain custom", model: "ollama/custom:latest", cards: []local.Accelerator{{Vendor: "nvidia", Name: "GPU", AvailableVRAM: local.Capacity{Bytes: 8 << 30, Known: true}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _, _ := newTestApp(t, "")
			a.probeHardware = func(context.Context, string) local.Hardware {
				return local.Hardware{Accelerators: tc.cards}
			}
			warning := a.localPlacementWarning(context.Background(), tc.model)
			view := tui.New(tui.Status{Model: tc.model, LocalWarning: warning}).View(48, 4)
			lines := strings.Split(view, "\n")
			if !strings.Contains(lines[len(lines)-1], "CPU") {
				t.Fatalf("48-column footer hides CPU in %q:\n%s", warning, view)
			}
		})
	}
}
