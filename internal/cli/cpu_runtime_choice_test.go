package cli

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/local"
)

// Settings affect the running tree even after setup publishes a different
// installation, or discovery finds the user's own server instead.
func TestCPUChoiceStillAppliesToAnOlderRunningTree(t *testing.T) {
	for _, discovery := range []string{"replacement", "absent", "user server"} {
		t.Run(discovery, func(t *testing.T) {
			a, _, _ := newTestApp(t, "")
			ctx := context.Background()
			dirs, err := a.locate()
			if err != nil {
				t.Fatal(err)
			}
			a.bindLocalRuntime(&config.Config{}, dirs, filepath.Join(t.TempDir(), "project"))
			host := local.Host{State: local.HostInstalled, Managed: true, Binary: "/old/ollama",
				MissingCompanion: "ROCm", CompanionVendor: "amd", CompanionFailure: "bad checksum"}
			a.localRuntime.Discover = func(context.Context) local.Host { return host }
			a.localRuntime.Port = func() (int, error) { return 43221, nil }
			a.localRuntime.Ready = func(context.Context, string) bool { return true }
			a.localRuntime.Start = func(context.Context, string, []string, []string) (local.Process, error) {
				return &localLifetimeProcess{}, nil
			}
			defer func() { _ = a.localRuntime.Close() }()
			if _, err := a.localRuntime.EnsureInstalled(ctx); err != nil {
				t.Fatal(err)
			}
			switch discovery {
			case "replacement":
				host = local.Host{State: local.HostInstalled, Managed: true, Binary: "/new/ollama"}
			case "absent":
				host = local.Host{}
			case "user server":
				host = local.Host{State: local.HostRunning, Addr: local.DefaultHostAddr}
			}
			for _, mode := range []string{"cpu", "auto", "cpu"} {
				if err := a.runConfig(ctx, []string{"set", "local.gpu_mode", mode}); err != nil {
					t.Fatal(err)
				}
				got := a.localHost(ctx)
				if got.Binary != "/old/ollama" || got.UnusableVendor() != "amd" {
					t.Fatalf("%s: running tree's facts changed: %+v", mode, got)
				}
				if mode == "cpu" && (got.MissingCompanion != "" || got.CompanionFailure != "" || got.AcceleratorNote != "") {
					t.Errorf("CPU chosen still reports a GPU problem: %+v", got)
				}
				if mode == "auto" && (got.MissingCompanion != "ROCm" || got.CompanionFailure != "bad checksum") {
					t.Errorf("automatic choice lost the running tree's facts: %+v", got)
				}
			}
		})
	}
}
