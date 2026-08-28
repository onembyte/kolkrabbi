package cli

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/engine"
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

// A login requested from inside a session is deferred, not refused: the
// provider CLI still must not be spawned while the input pump owns the
// keyboard, but the user should not have to open a second terminal either.
func TestPlansLoginDefersTheHandoverWhileKolkrabbiOwnsTheTerminal(t *testing.T) {
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
	if a.pendingLogin == nil || a.pendingLogin.Name != "Claude Max" {
		t.Fatalf("login was not armed for after the session: %+v", a.pendingLogin)
	}
	if got := out.String(); !strings.Contains(got, "come back to this session") {
		t.Fatalf("output does not say the session is resumed: %q", got)
	}
	manifest, err := provider.LoadConnectors(dirs.ConnectorsFile())
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Connectors) != 0 {
		t.Fatalf("a deferred login enabled connectors before signing in: %+v", manifest.Connectors)
	}
}

// And once the screen is down, finishSession performs it and records the
// connector — the whole point of deferring rather than refusing.
func TestFinishSessionRunsTheDeferredLoginAndComesBack(t *testing.T) {
	dirs := isolateConnectorState(t)
	a, _, _ := newTestApp(t, "")

	var spawned string
	a.handover = func(_ context.Context, executable string, _ []string, _ string) error {
		spawned = executable
		return nil
	}
	var restarted bool
	a.executablePath = func() (string, error) { return "/usr/local/bin/kolk", nil }
	a.replaceSelf = func(string, []string, []string) error { restarted = true; return nil }

	plans := provider.Plans("anthropic")
	var selected provider.Plan
	for _, plan := range plans {
		if plan.Name == "Claude Max" {
			selected = plan
		}
	}
	a.pendingLogin = &selected

	a.finishSession(context.Background(), &engine.Agent{})

	if spawned != "claude" {
		t.Fatalf("handover spawned %q, want the claude CLI", spawned)
	}
	manifest, err := provider.LoadConnectors(dirs.ConnectorsFile())
	if err != nil || len(manifest.Connectors) != 1 || !manifest.Connectors[0].Enabled {
		t.Fatalf("connector after login = %+v, err=%v", manifest.Connectors, err)
	}
	if manifest.Connectors[0].Verified {
		t.Fatal("a clean exit is not proof of a login and must not be recorded as verified")
	}
	if !restarted {
		t.Fatal("the session was not resumed after the login")
	}
	if a.pendingLogin != nil {
		t.Fatal("the pending login outlived the login it described")
	}
}

func TestSlashPlanLoginEndsTheSessionSoTheHandoverCanHappen(t *testing.T) {
	isolateConnectorState(t)
	a, ag, out := replFixture(t, "")
	a.terminalOwned = func() bool { return true }
	a.handover = func(context.Context, string, []string, string) error {
		t.Fatal("/plogin must never spawn a provider login inside the TUI")
		return nil
	}

	if !a.slash(context.Background(), ag, "/plogin anthropic Claude Max") {
		t.Fatal("/plogin must end the session so the provider CLI gets the keyboard")
	}
	if a.pendingLogin == nil {
		t.Fatal("/plogin did not arm the login")
	}
	if got := out.String(); !strings.Contains(got, "come back to this session") {
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
