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
	a, out, errOut := newTestApp(t, "")
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
		LoginOwner: "provider-cli", Enabled: true, Verified: true,
	}); err != nil {
		t.Fatal(err)
	}
	a, out, errOut := newTestApp(t, "")
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
	a, out, errOut := newTestApp(t, "")
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
	if gotExecutable != "claude" || !strings.Contains(out.String(), "Claude Max recorded") {
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

func TestPlansLoginRefusesHandoverWhileKolkrabbiOwnsTheTerminal(t *testing.T) {
	dirs := isolateConnectorState(t)
	a, out, errOut := newTestApp(t, "")
	a.terminalOwned = func() bool { return true }
	a.handover = func(context.Context, string, []string, string) error {
		t.Fatal("a provider login must never be spawned while Kolkrabbi owns the terminal")
		return nil
	}

	if code := a.main(context.Background(), []string{"plans", "login", "anthropic", "Claude", "Max"}); code != ExitOK {
		t.Fatalf("plans login exit = %d, stderr = %q", code, errOut.String())
	}

	got := out.String()
	if !strings.Contains(got, `kolk plans login anthropic "Claude Max"`) {
		t.Fatalf("output does not tell the user the exact command to run elsewhere: %q", got)
	}
	if !strings.Contains(got, "separate terminal") {
		t.Fatalf("output does not explain why the login moves terminals: %q", got)
	}
	manifest, err := provider.LoadConnectors(dirs.ConnectorsFile())
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Connectors) != 0 {
		t.Fatalf("a refused login enabled connectors anyway: %+v", manifest.Connectors)
	}
}

func TestSlashPlanLoginRefusesHandoverWhileKolkrabbiOwnsTheTerminal(t *testing.T) {
	isolateConnectorState(t)
	a, ag, out := replFixture(t, "")
	a.terminalOwned = func() bool { return true }
	a.handover = func(context.Context, string, []string, string) error {
		t.Fatal("/plogin must never spawn a provider login inside the TUI")
		return nil
	}

	if a.slash(context.Background(), ag, "/plogin anthropic Claude Max") {
		t.Fatal("/plogin must not exit the session")
	}
	if got := out.String(); !strings.Contains(got, `kolk plans login anthropic "Claude Max"`) {
		t.Fatalf("slash plogin output = %q", got)
	}
}

// isolateConnectorState keeps every plan test off the developer's real
// connector manifest. Without it a login test writes a fake enabled connector
// into the machine running the suite.
func isolateConnectorState(t *testing.T) paths.Dirs {
	t.Helper()
	base := t.TempDir()
	t.Setenv(paths.EnvDataDir, filepath.Join(base, "data"))
	t.Setenv(paths.EnvConfigDir, filepath.Join(base, "config"))
	t.Setenv(paths.EnvCacheDir, filepath.Join(base, "cache"))
	dirs, err := paths.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	return dirs
}

// A provider CLI that exits 0 has not proved anything: the user may have quit
// the login without signing in. Kolkrabbi records what it saw, not what it
// hopes happened.
func TestPlansLoginRecordsAnUnverifiedConnector(t *testing.T) {
	dirs := isolateConnectorState(t)
	a, out, errOut := newTestApp(t, "")
	a.handover = func(context.Context, string, []string, string) error { return nil }

	if code := a.main(context.Background(), []string{"plans", "login", "anthropic", "Claude", "Max"}); code != ExitOK {
		t.Fatalf("plans login exit = %d, stderr = %q", code, errOut.String())
	}

	manifest, err := provider.LoadConnectors(dirs.ConnectorsFile())
	if err != nil || len(manifest.Connectors) != 1 {
		t.Fatalf("manifest = %+v, err = %v", manifest, err)
	}
	connector := manifest.Connectors[0]
	if !connector.Enabled {
		t.Fatal("a clean login must record the connector")
	}
	if connector.Verified {
		t.Fatal("a clean exit was recorded as a verified login")
	}
	if !strings.Contains(out.String(), "not proof") {
		t.Fatalf("output = %q, want it to say what a clean exit does and does not prove", out.String())
	}
}

func TestPlansMarksAnUnverifiedConnectorAsSuch(t *testing.T) {
	dirs := isolateConnectorState(t)
	if err := provider.SaveConnector(context.Background(), dirs.ConnectorsFile(), provider.Connector{
		Provider: "anthropic", Plan: "Claude Max", Name: "claude",
		LoginOwner: "provider-cli", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	a, out, errOut := newTestApp(t, "")

	if code := a.main(context.Background(), []string{"plans", "claude"}); code != ExitOK {
		t.Fatalf("plans exit = %d, stderr = %q", code, errOut.String())
	}
	got := out.String()
	if !strings.Contains(got, "unverified") {
		t.Fatalf("plans output = %q, want the unverified state", got)
	}
	if !strings.Contains(got, "answers a turn") {
		t.Fatalf("plans output = %q, want an explanation of what unverified means", got)
	}
}
