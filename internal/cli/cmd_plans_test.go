package cli

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/paths"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

func TestPlansListsAndFiltersProviderPlans(t *testing.T) {
	a, out, errOut := newTestApp("")
	if code := a.main(context.Background(), []string{"plans", "gemini"}); code != ExitOK {
		t.Fatalf("plans exit = %d, stderr = %q", code, errOut.String())
	}

	got := out.String()
	if !strings.Contains(got, "Google AI Pro") || strings.Contains(got, "Claude Max") {
		t.Fatalf("filtered plans output = %q", got)
	}
}

func TestSlashPlansListsAndFiltersProviderPlans(t *testing.T) {
	a, ag, out := replFixture(t, "")
	if a.slash(context.Background(), ag, "/plans pro") {
		t.Fatal("/plans must not exit the REPL")
	}
	if got := out.String(); !strings.Contains(got, "Google AI Pro") ||
		!strings.Contains(got, "ChatGPT Pro") {
		t.Fatalf("slash plans output = %q", got)
	}
}

func TestPlansShowsEnabledConnectorStatus(t *testing.T) {
	base := t.TempDir()
	t.Setenv(paths.EnvDataDir, filepath.Join(base, "data"))
	t.Setenv(paths.EnvConfigDir, filepath.Join(base, "config"))
	t.Setenv(paths.EnvCacheDir, filepath.Join(base, "cache"))
	dirs := paths.Dirs{Data: filepath.Join(base, "data")}
	if err := dirs.EnsureData(); err != nil {
		t.Fatal(err)
	}
	if err := provider.SaveConnector(context.Background(), dirs.ConnectorsFile(), provider.Connector{
		Provider: "google", Plan: "Google AI Pro", Name: "gemini",
		LoginOwner: "provider-cli", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	a, out, errOut := newTestApp("")
	if code := a.main(context.Background(), []string{"plans", "gemini"}); code != ExitOK {
		t.Fatalf("plans exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "enabled") {
		t.Fatalf("plans output does not show enabled connector: %q", out.String())
	}
}

func TestPlansLoginUsesHandoverAndPersistsMetadata(t *testing.T) {
	base := t.TempDir()
	t.Setenv(paths.EnvDataDir, filepath.Join(base, "data"))
	t.Setenv(paths.EnvConfigDir, filepath.Join(base, "config"))
	t.Setenv(paths.EnvCacheDir, filepath.Join(base, "cache"))
	a, out, errOut := newTestApp("")
	var gotExecutable string
	a.handover = func(_ context.Context, executable string, args []string, dir string) error {
		gotExecutable = executable
		if len(args) != 0 || dir != "" {
			t.Fatalf("handover args=%v dir=%q, want no provider credential inputs", args, dir)
		}
		return nil
	}
	if code := a.main(context.Background(), []string{"plans", "login", "anthropic", "Claude", "Max"}); code != ExitOK {
		t.Fatalf("plans login exit = %d, stderr = %q", code, errOut.String())
	}
	if gotExecutable != "claude" || !strings.Contains(out.String(), "connector enabled") {
		t.Fatalf("handover/output = %q, executable=%q", out.String(), gotExecutable)
	}
	dirs, err := paths.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := provider.LoadConnectors(dirs.ConnectorsFile())
	if err != nil || len(manifest.Connectors) != 1 || !manifest.Connectors[0].Enabled {
		t.Fatalf("saved connector = %+v, err=%v", manifest.Connectors, err)
	}
}
