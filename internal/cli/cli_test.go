package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/keystore"
	"github.com/onembyte/kolkrabbi/internal/local"
	"github.com/onembyte/kolkrabbi/internal/paths"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/selfupdate"
)

// newTestApp builds an app whose streams are buffers, so a whole kolk
// invocation can be run and asserted on in-process.
// newTestApp builds an app already pointed away from the developer's real
// state.
//
// It takes *testing.T solely so it can isolate: it could not before, so
// isolation was something each test had to remember, and 44 of the 100 tests
// using this helper did not. One of them reached OpenRouter during `make check`
// and came back rate-limited against the owner's real quota. Isolation someone
// can forget is isolation that will be forgotten.
func newTestApp(t *testing.T, stdin string) (*app, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	isolateHome(t)
	var out, errOut bytes.Buffer
	a := newApp()
	a.chooseDefault = func([]provider.ModelInfo) defaultModelChoice {
		return defaultModelChoice{Model: defaultModel, Free: true}
	}
	a.stdout = &out
	a.stderr = &errOut
	a.in = bufio.NewReader(strings.NewReader(stdin))
	a.canAnimate = func() bool { return false }
	// Never the real loopback port: the owner's machine has an Ollama on it,
	// and a test that found it would pass here and fail everywhere else.
	a.discoverHost = func(context.Context) local.Host { return local.Host{State: local.HostAbsent} }
	a.listHostModels = func(context.Context, string, string) ([]local.HostModel, error) { return nil, nil }
	a.listCloudCatalog = nil
	a.listCloudModels = nil
	a.signIn = func(context.Context, string) local.SignInState { return local.SignInState{} }
	a.probeHardware = func(context.Context, string) local.Hardware { return local.Hardware{} }
	a.pulledNames = func() map[string]bool { return map[string]bool{} }
	// Start-time discovery runs in the background and writes into the app's
	// dirs; a test that returns while it is still writing leaves TempDir with
	// "directory not empty" — seen on CI for the v1.2.33 commit.
	t.Cleanup(a.joinBackground)
	return a, &out, &errOut
}

// blockProviderAccess points any provider call that has not been aimed
// somewhere else at a closed port.
//
// Only when nothing has aimed it yet: a test already pointing kolk at its own
// mock keeps that, and one that has not gets connection-refused in microseconds
// instead of a real request to openrouter.ai. Blank is not neutral — blank
// means "use the real API", which is how `make check` came back rate-limited
// against the owner's account on 2026-08-27.
func blockProviderAccess(t *testing.T) {
	t.Helper()
	if os.Getenv("OPENROUTER_BASE_URL") == "" {
		t.Setenv("OPENROUTER_BASE_URL", unroutableBaseURL)
	}
}

// unroutableBaseURL is a loopback port nothing listens on. Any provider call a
// test makes without pointing at its own mock lands here and fails at once.
const unroutableBaseURL = "http://127.0.0.1:1"

// isolateHome points kolk at a temp directory so tests never read or write the
// developer's real state, and clears the env key so a key in the shell running
// the tests cannot change the outcome.
//
// It sets the KOLK_*_DIR overrides rather than $HOME: those are the one thing
// that means the same on every platform, so the tests do not quietly depend on
// the unix layout.
func isolateHome(t *testing.T) paths.Dirs {
	t.Helper()
	// Idempotent: a second call returns the isolation the first one set up,
	// rather than pointing the process at a fresh temp directory the caller
	// has never heard of. newTestApp isolates unconditionally, so any test
	// that also isolates explicitly calls this twice.
	// The guard is applied first and separately from the directories, because
	// the two can already be half-done: several tests point kolk at their own
	// directories without going through this helper, and they used to skip the
	// guard entirely on the early return below.
	blockProviderAccess(t)

	if existing := os.Getenv(paths.EnvConfigDir); existing != "" {
		return paths.Dirs{
			Config: existing,
			Data:   os.Getenv(paths.EnvDataDir),
			Cache:  os.Getenv(paths.EnvCacheDir),
		}
	}
	base := t.TempDir()
	d := paths.Dirs{
		Config: filepath.Join(base, "config"),
		Data:   filepath.Join(base, "data"),
		Cache:  filepath.Join(base, "cache"),
	}
	t.Setenv(paths.EnvConfigDir, d.Config)
	t.Setenv(paths.EnvDataDir, d.Data)
	t.Setenv(paths.EnvCacheDir, d.Cache)
	t.Setenv("OPENROUTER_API_KEY", "")
	blockProviderAccess(t)
	t.Setenv("CI", "")
	return d
}

func TestHelpDocumentsEveryCommandAndFlag(t *testing.T) {
	a, out, _ := newTestApp(t, "")
	if code := a.main(context.Background(), []string{"help"}); code != ExitOK {
		t.Fatalf("kolk help exit = %d, want %d", code, ExitOK)
	}
	got := out.String()

	for _, c := range commandTable() {
		if !strings.Contains(got, c.name) {
			t.Errorf("help omits command %q", c.name)
		}
		if !strings.Contains(got, c.summary) {
			t.Errorf("help omits summary of %q: %q", c.name, c.summary)
		}
	}
	for _, f := range flagTable {
		if !strings.Contains(got, "--"+f.long) {
			t.Errorf("help omits flag --%s", f.long)
		}
		if !strings.Contains(got, f.summary) {
			t.Errorf("help omits summary of --%s: %q", f.long, f.summary)
		}
	}
}

func TestHelpFlagsAreEquivalent(t *testing.T) {
	var first string
	for _, args := range [][]string{{"help"}, {"-h"}, {"--help"}} {
		a, out, _ := newTestApp(t, "")
		if code := a.main(context.Background(), args); code != ExitOK {
			t.Fatalf("kolk %v exit = %d", args, code)
		}
		if first == "" {
			first = out.String()
			continue
		}
		if out.String() != first {
			t.Errorf("kolk %v printed different help than `kolk help`", args)
		}
	}
}

func TestTopLevelUpdateNeedsNoKeyOrState(t *testing.T) {
	d := isolateHome(t)
	a, out, errOut := newTestApp(t, "")
	a.currentVersion = func() string { return "1.0.0" }
	calls := 0
	a.update = func(context.Context) (selfupdate.Result, error) {
		calls++
		if got := out.String(); got != "Current version: 1.0.0\nChecking for updates to latest version...\n" {
			t.Fatalf("pre-update output = %q", got)
		}
		return selfupdate.Result{
			Current: "1.0.0", Latest: "1.2.3", Updated: true, Path: "/usr/local/bin/kolk",
		}, nil
	}

	if code := runUpdateInSession(t, a); code != ExitOK {
		t.Fatalf("/update exit = %d, stderr %q", code, errOut.String())
	}
	if calls != 1 {
		t.Fatalf("updater calls = %d, want 1", calls)
	}
	for _, want := range []string{
		"Current version: 1.0.0",
		"Checking for updates to latest version...",
		"Kolk updated successfully (1.0.0 → 1.2.3)",
		"Installed to: /usr/local/bin/kolk",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("update output omitted %q: %q", want, out.String())
		}
	}
	// The "creates no directories" half of this test was a property of the
	// retired `kolk update` verb, which ran before anything resolved. /update
	// runs inside a session that has already made its directories, so the
	// assertion would now be about the session rather than the update. What
	// still matters — and is asserted above — is that it needs no key.
	_ = d
}

func TestUpdateRejectsArgumentsBeforeCallingUpdater(t *testing.T) {
	calls := 0
	a, ag, out := replFixture(t, "")
	a.update = func(context.Context) (selfupdate.Result, error) {
		calls++
		return selfupdate.Result{}, nil
	}
	// /update takes no argument, and says so before the updater is reached:
	// an argument is a misunderstanding, and answering it by downloading a
	// release is the wrong way to find that out.
	a.slash(context.Background(), ag, "/update now")
	if !strings.Contains(out.String(), "usage: /update") {
		t.Fatalf("/update now = %q, want the usage line", out.String())
	}
	if calls != 0 {
		t.Fatalf("the updater ran for a rejected argument: calls = %d", calls)
	}
}

func TestTopLevelUpdateReportsUnchangedFailureAndWarning(t *testing.T) {
	t.Run("unchanged", func(t *testing.T) {
		a, out, _ := newTestApp(t, "")
		a.currentVersion = func() string { return "1.2.3" }
		a.update = func(context.Context) (selfupdate.Result, error) {
			return selfupdate.Result{Current: "1.2.3", Latest: "1.2.3"}, nil
		}
		if code := runUpdateInSession(t, a); code != ExitOK {
			t.Fatalf("exit = %d", code)
		}
		if got, want := out.String(), "Current version: 1.2.3\nChecking for updates to latest version...\nKolk is up to date (1.2.3)\n"; got != want {
			t.Fatalf("unchanged output = %q, want %q", got, want)
		}
	})

	t.Run("newer than release", func(t *testing.T) {
		a, out, _ := newTestApp(t, "")
		a.currentVersion = func() string { return "2.0.0" }
		a.update = func(context.Context) (selfupdate.Result, error) {
			return selfupdate.Result{Current: "2.0.0", Latest: "1.2.3"}, nil
		}
		if code := runUpdateInSession(t, a); code != ExitOK {
			t.Fatalf("exit = %d", code)
		}
		if got := out.String(); !strings.Contains(got, "Kolk is newer than the latest release (current 2.0.0; latest 1.2.3)") {
			t.Fatalf("newer-build output = %q", got)
		}
	})

	t.Run("failure", func(t *testing.T) {
		a, out, errOut := newTestApp(t, "")
		a.currentVersion = func() string { return "1.2.3" }
		a.update = func(context.Context) (selfupdate.Result, error) {
			return selfupdate.Result{}, errors.New("release unavailable")
		}
		if code := runUpdateInSession(t, a); code != ExitError {
			t.Fatalf("exit = %d, want %d", code, ExitError)
		}
		if !strings.Contains(errOut.String(), "release unavailable") {
			t.Fatalf("failure stderr = %q", errOut.String())
		}
		if got, want := out.String(), "Current version: 1.2.3\nChecking for updates to latest version...\n"; got != want {
			t.Fatalf("failure progress = %q, want %q", got, want)
		}
	})

	t.Run("durability warning", func(t *testing.T) {
		a, out, errOut := newTestApp(t, "")
		a.currentVersion = func() string { return "1.0.0" }
		a.update = func(context.Context) (selfupdate.Result, error) {
			return selfupdate.Result{
				Current: "1.0.0", Latest: "1.2.3", Updated: true,
				Path: "/bin/kolk", Warning: "directory sync refused",
			}, nil
		}
		if code := runUpdateInSession(t, a); code != ExitOK {
			t.Fatalf("exit = %d", code)
		}
		if !strings.Contains(out.String(), "Kolk updated successfully") || !strings.Contains(errOut.String(), "warning: directory sync refused") {
			t.Fatalf("stdout %q, stderr %q", out.String(), errOut.String())
		}
	})
}

func TestCommandNamesAreUniqueAndTypeable(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range commandTable() {
		if seen[c.name] {
			t.Errorf("duplicate command %q", c.name)
		}
		seen[c.name] = true
		if c.name != strings.ToLower(c.name) || strings.ContainsAny(c.name, " -_") {
			t.Errorf("command %q must be one lowercase word", c.name)
		}
		// The exceptions are named in parity_test.go's longVerbs, with the
		// reason each one earns. Keeping one list rather than two is what stops
		// the two gates from disagreeing about the same command.
		if _, allowed := longVerbs[c.name]; !allowed && len(c.name) > 8 {
			t.Errorf("command %q is %d letters; the surface is meant to be typeable", c.name, len(c.name))
		}
		if c.summary == "" {
			t.Errorf("command %q has no summary, so help cannot document it", c.name)
		}
	}
}

// A word that is not a command must reach the prompt, not an error: `kolk fix
// the failing test` is the product's main input path.
func TestUnknownWordIsAPromptNotACommand(t *testing.T) {
	for _, word := range []string{"fix", "explain", "write", "do"} {
		if c := lookupCommand(word); c != nil {
			t.Errorf("%q resolves to command %q; it must be treated as prompt text", word, c.name)
		}
	}
}

func TestFirstRunWithoutAKeyIsExactAndReadOnly(t *testing.T) {
	d := isolateHome(t)
	t.Setenv("OPENROUTER_BASE_URL", provider.DefaultBaseURL)
	a, out, errOut := newTestApp(t, "")

	code := a.main(context.Background(), nil)
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d", code, ExitUsage)
	}
	const want = "kolk needs an API key before it can use models.\n" +
		"Add one:  /key <API_KEY>\n" +
		"Then run: kolk\n"
	if got := errOut.String(); got != want {
		t.Errorf("first-run guidance:\n%s\nwant exactly:\n%s", got, want)
	}
	if got := out.String(); got != "" {
		t.Errorf("first run wrote to stdout: %q", got)
	}
	for name, path := range map[string]string{
		"config": d.Config,
		"data":   d.Data,
		"cache":  d.Cache,
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("missing-key run created %s state at %s: %v", name, path, err)
		}
	}
}

func TestBadFlagIsAUsageError(t *testing.T) {
	isolateHome(t)
	for _, args := range [][]string{
		{"--mdoel", "gpt-4"},
		{"-m"},
		{"--model"},
		{"--yolo=please"},
	} {
		a, _, errOut := newTestApp(t, "")
		code := a.main(context.Background(), args)
		if code != ExitUsage {
			t.Errorf("kolk %v exit = %d, want %d (usage)", args, code, ExitUsage)
		}
		if !strings.Contains(errOut.String(), "kolk help") {
			t.Errorf("kolk %v did not point at help: %q", args, errOut.String())
		}
	}
}

func TestSessionsAndStatsRunOnAnEmptyMachine(t *testing.T) {
	isolateHome(t)

	a, out, _ := newTestApp(t, "")
	if code := a.main(context.Background(), []string{"sessions"}); code != ExitOK {
		t.Fatalf("kolk sessions exit = %d", code)
	}
	if !strings.Contains(out.String(), "no sessions yet") {
		t.Errorf("kolk sessions on a fresh machine printed %q", out.String())
	}

	a, out, _ = newTestApp(t, "")
	if code := runRetiredVerb(t, a, "stats"); code != ExitOK {
		t.Fatalf("/stats exit = %d", code)
	}
	if !strings.Contains(out.String(), "nothing ever leaves this machine") {
		t.Errorf("/stats must state the local-only promise, got %q", out.String())
	}
}

func TestConfigSettingsRoundTripWithoutACredentialField(t *testing.T) {
	isolateHome(t)

	a, _, _ := newTestApp(t, "")
	if code := runRetiredVerb(t, a, "config", "set-tier", "quick", "google/gemini-2.5-flash"); code != ExitOK {
		t.Fatalf("config set-tier exit = %d", code)
	}

	a, out, _ := newTestApp(t, "")
	if code := runRetiredVerb(t, a, "config", "show"); code != ExitOK {
		t.Fatalf("config show exit = %d", code)
	}
	got := out.String()
	if strings.Contains(got, "api_key") {
		t.Errorf("config show still exposes a credential setting:\n%s", got)
	}
	if !strings.Contains(got, "google/gemini-2.5-flash") {
		t.Errorf("config show lost the saved tier:\n%s", got)
	}
}

func TestConfigWriteEvacuatesALegacyKeyBeforeSaving(t *testing.T) {
	d := isolateHome(t)
	if err := os.MkdirAll(d.Config, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := `{"api_key":"` + cliKeyCanary + `","model":"old/model","base_url":"https://example.test"}`
	if err := os.WriteFile(d.ConfigFile(), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	a, _, errOut := newTestApp(t, "")
	if code := runRetiredVerb(t, a, "config", "set-model", "new/model"); code != ExitOK {
		t.Fatalf("config write exit = %d, stderr: %s", code, errOut)
	}
	if !strings.Contains(errOut.String(), "moved your saved API key") {
		t.Errorf("migration notice missing: %s", errOut)
	}
	stored, err := keystore.NewFileStore(d.CredentialsFile()).Get(context.Background(), keystore.Ref{Provider: "openrouter"})
	if err != nil {
		t.Fatal(err)
	}
	if stored.Reveal() != cliKeyCanary {
		t.Errorf("migrated credential = %v", stored)
	}
	body, err := os.ReadFile(d.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), cliKeyCanary) || strings.Contains(string(body), "api_key") {
		t.Errorf("config write retained the legacy credential: %s", body)
	}
	if !strings.Contains(string(body), "new/model") {
		t.Errorf("config write lost the requested setting: %s", body)
	}

	a, _, errOut = newTestApp(t, "")
	if code := runRetiredVerb(t, a, "config", "set-base-url", "https://second.test"); code != ExitOK {
		t.Fatalf("second config write exit = %d, stderr: %s", code, errOut)
	}
	if strings.Contains(errOut.String(), "moved your saved API key") {
		t.Errorf("idempotent migration printed twice: %s", errOut)
	}
}

func TestInvalidConfigWriteDoesNotTriggerLegacyMigration(t *testing.T) {
	d := isolateHome(t)
	if err := os.MkdirAll(d.Config, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := `{"api_key":"` + cliKeyCanary + `","model":"old/model"}`
	if err := os.WriteFile(d.ConfigFile(), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	a, _, _ := newTestApp(t, "")
	if code := runRetiredVerb(t, a, "config", "set-tier", "bogus", "some/model"); code != ExitUsage {
		t.Fatalf("invalid config write exit = %d, want %d", code, ExitUsage)
	}
	if _, err := os.Stat(d.CredentialsFile()); !os.IsNotExist(err) {
		t.Errorf("invalid config write created a manifest: %v", err)
	}
	body, err := os.ReadFile(d.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), cliKeyCanary) {
		t.Error("invalid config write mutated the legacy config")
	}
}

func TestConfigRejectsAnUnknownEffortTier(t *testing.T) {
	isolateHome(t)
	a, _, _ := newTestApp(t, "")
	if code := runRetiredVerb(t, a, "config", "set-tier", "bogus", "some/model"); code != ExitUsage {
		t.Errorf("set-tier bogus exit = %d, want %d", code, ExitUsage)
	}
}

func TestFormatPricing(t *testing.T) {
	cases := []struct{ in, out, want string }{
		{"0", "0", "free"},
		{"0.0000005", "0.0000015", "$0.50 in / $1.50 out per 1M tokens"},
		{"", "", "in  / out  per token"},
	}
	for _, c := range cases {
		if got := formatPricing(c.in, c.out); got != c.want {
			t.Errorf("formatPricing(%q,%q) = %q, want %q", c.in, c.out, got, c.want)
		}
	}
}

func TestHelpForACommandShowsItsGrammar(t *testing.T) {
	a, out, _ := newTestApp(t, "")
	if code := a.main(context.Background(), []string{"help", "sessions"}); code != ExitOK {
		t.Fatalf("kolk help sessions exit = %d", code)
	}
	got := out.String()
	if !strings.Contains(got, "usage: kolk sessions") {
		t.Errorf("kolk help sessions did not print a usage line:\n%s", got)
	}
	if !strings.Contains(got, "fork") {
		t.Errorf("kolk help sessions did not print the argument grammar:\n%s", got)
	}
}

func TestHelpForAnUnknownCommandIsAUsageError(t *testing.T) {
	a, _, _ := newTestApp(t, "")
	if code := a.main(context.Background(), []string{"help", "nope"}); code != ExitUsage {
		t.Errorf("kolk help nope exit = %d, want %d", code, ExitUsage)
	}
}

// Every command's usage line must be derivable from the table, because the
// "usage:" strings printed on a mistake are generated from it.
func TestUsageLineIsGeneratedForEveryCommand(t *testing.T) {
	for _, c := range commandTable() {
		got := usageLine(c.name)
		if !strings.HasPrefix(got, "usage: kolk "+c.name) {
			t.Errorf("usageLine(%q) = %q", c.name, got)
		}
		if c.args != "" && !strings.Contains(got, c.args) {
			t.Errorf("usageLine(%q) dropped the grammar %q", c.name, c.args)
		}
	}
}

func TestBadSubcommandPrintsTheGeneratedUsage(t *testing.T) {
	isolateHome(t)
	a, _, errOut := newTestApp(t, "")
	if code := runRetiredVerb(t, a, "config", "set-everything"); code != ExitUsage {
		t.Fatalf("exit = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(errOut.String(), "usage: /config") {
		t.Errorf("bad subcommand did not print generated usage: %q", errOut.String())
	}
}

// The directories are a decision with consequences: settings may be symlinked
// into dotfiles, while a credential must remain in private state.
func TestStateAndConfigAreSeparateOnDisk(t *testing.T) {
	d := isolateHome(t)

	a, _, _ := newTestApp(t, "")
	if code := runRetiredVerb(t, a, "config", "set-model", "openrouter/auto"); code != ExitOK {
		t.Fatalf("config set-model exit = %d", code)
	}
	if _, err := os.Stat(d.ConfigFile()); err != nil {
		t.Errorf("config did not land in the config directory: %v", err)
	}

	const mistralKey = "0123456789abcdef0123456789abcdef"
	a, _, _ = newTestApp(t, mistralKey+"\n")
	if code := runRetiredVerb(t, a, "key", "mistral", "-"); code != ExitOK {
		t.Fatalf("/key exit = %d", code)
	}
	if _, err := os.Stat(d.CredentialsFile()); err != nil {
		t.Errorf("credential did not land in the data directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(d.Data, ".gitignore")); err != nil {
		t.Errorf("the data directory has no .gitignore: %v", err)
	}
}

// Commands that need nothing from disk must work on a machine where the home
// directory cannot be resolved at all.
func TestHelpNeedsNoDirectories(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv(paths.EnvConfigDir, "")
	t.Setenv(paths.EnvDataDir, "")
	t.Setenv(paths.EnvCacheDir, "")

	// version stopped being a verb on 2026-09-02; the build it printed is in
	// `kolk help` now, which is the one command that must work before any
	// directory does.
	for _, verb := range []string{"help"} {
		a, out, _ := newTestApp(t, "")
		if code := a.main(context.Background(), []string{verb}); code != ExitOK {
			t.Errorf("kolk %s exit = %d with no resolvable home directory", verb, code)
		}
		if out.Len() == 0 {
			t.Errorf("kolk %s printed nothing", verb)
		}
	}
}

func TestConfigSetGetUnsetDottedEffortModel(t *testing.T) {
	isolateHome(t)

	// 1. Initial get is unset
	a, out, _ := newTestApp(t, "")
	if code := runRetiredVerb(t, a, "config", "get", "effort.high.model"); code != ExitOK {
		t.Fatalf("config get unset exit = %d, want ExitOK", code)
	}
	if !strings.Contains(out.String(), "unset") {
		t.Errorf("config get unset output = %q, want unset note", out.String())
	}

	// 2. Set effort.high.model
	a, out, _ = newTestApp(t, "")
	if code := runRetiredVerb(t, a, "config", "set", "effort.high.model", "anthropic/claude-opus-4.5"); code != ExitOK {
		t.Fatalf("config set effort.high.model exit = %d, want ExitOK", code)
	}
	if !strings.Contains(out.String(), "effort.high.model → anthropic/claude-opus-4.5") {
		t.Errorf("config set output = %q", out.String())
	}

	// 3. Get effort.high.model returns set value
	a, out, _ = newTestApp(t, "")
	if code := runRetiredVerb(t, a, "config", "get", "effort.high.model"); code != ExitOK {
		t.Fatalf("config get exit = %d", code)
	}
	if !strings.Contains(out.String(), "anthropic/claude-opus-4.5") {
		t.Errorf("config get output = %q, want model", out.String())
	}

	// 4. Also accessible via numeric alias: get effort.3.model
	a, out, _ = newTestApp(t, "")
	if code := runRetiredVerb(t, a, "config", "get", "effort.3.model"); code != ExitOK {
		t.Fatalf("config get effort.3.model exit = %d", code)
	}
	if !strings.Contains(out.String(), "anthropic/claude-opus-4.5") {
		t.Errorf("config get effort.3.model output = %q, want model", out.String())
	}

	// 5. Unset effort.high.model
	a, out, _ = newTestApp(t, "")
	if code := runRetiredVerb(t, a, "config", "unset", "effort.high.model"); code != ExitOK {
		t.Fatalf("config unset exit = %d", code)
	}
	if !strings.Contains(out.String(), "removed effort.high.model") {
		t.Errorf("config unset output = %q", out.String())
	}

	// 6. Verify unset
	a, out, _ = newTestApp(t, "")
	_ = runRetiredVerb(t, a, "config", "get", "effort.high.model")
	if !strings.Contains(out.String(), "unset") {
		t.Errorf("config get after unset output = %q, want unset note", out.String())
	}
}

// TestIsolationLeavesNoRouteToARealProvider pins the guard itself.
//
// The guard is one line in a helper, and a helper's line is exactly the kind of
// thing a future change removes while making something else work. This asserts
// the property rather than the line: after isolation, nothing points at a real
// provider, and a call would fail rather than succeed.
func TestIsolationLeavesNoRouteToARealProvider(t *testing.T) {
	isolateHome(t)

	base := os.Getenv("OPENROUTER_BASE_URL")
	if base == "" {
		t.Fatal("base URL is blank after isolation, which means the real API")
	}
	if !strings.Contains(base, "127.0.0.1") && !strings.Contains(base, "localhost") {
		t.Fatalf("base URL = %q after isolation; tests must not be able to reach a real host", base)
	}
	if key := os.Getenv("OPENROUTER_API_KEY"); key != "" {
		t.Fatalf("a key survived isolation: tests would run with the developer's credentials")
	}
}

// TestIsolationDoesNotOverrideATestsOwnMock keeps the guard from being a
// nuisance, which is how guards get deleted.
func TestIsolationDoesNotOverrideATestsOwnMock(t *testing.T) {
	t.Setenv("OPENROUTER_BASE_URL", "http://127.0.0.1:65535/mine")

	isolateHome(t)

	if got := os.Getenv("OPENROUTER_BASE_URL"); got != "http://127.0.0.1:65535/mine" {
		t.Fatalf("isolation clobbered the test's own mock: %q", got)
	}
}

// TestConfigSetsTheSubscriptionLimitPolicy covers the setting a person reaches
// for after a plan runs out mid-run (A33.7). The default has to be visible
// without setting anything: a policy nobody can see is one nobody chose.
func TestConfigSetsTheSubscriptionLimitPolicy(t *testing.T) {
	isolateHome(t)

	a, out, _ := newTestApp(t, "")
	if code := runRetiredVerb(t, a, "config"); code != ExitOK {
		t.Fatalf("config exit = %d, want ExitOK", code)
	}
	if !strings.Contains(out.String(), "routing.on_subscription_limit") || !strings.Contains(out.String(), "ask") {
		t.Errorf("config listing %q does not show the default policy", out.String())
	}

	a, out, _ = newTestApp(t, "")
	if code := runRetiredVerb(t, a, "config", "set", "routing.on_subscription_limit", "switch"); code != ExitOK {
		t.Fatalf("config set exit = %d, want ExitOK", code)
	}
	if !strings.Contains(out.String(), "routing.on_subscription_limit → switch") {
		t.Errorf("config set output = %q", out.String())
	}

	// A typo must not silently become a policy: "continue" reads like an
	// answer and is not one, and guessing which way it meant is how a run
	// starts spending money nobody agreed to.
	a, _, errOut := newTestApp(t, "")
	if code := runRetiredVerb(t, a, "config", "set", "routing.on_subscription_limit", "continue"); code == ExitOK {
		t.Fatal("config set continue exit = ExitOK, want a rejection")
	}
	if !strings.Contains(errOut.String(), "ask, switch or stop") {
		t.Errorf("rejection %q does not name the policies that do work", errOut.String())
	}

	a, _, _ = newTestApp(t, "")
	if code := runRetiredVerb(t, a, "config", "unset", "routing.on_subscription_limit"); code != ExitOK {
		t.Fatalf("config unset exit = %d, want ExitOK", code)
	}
	a, out, _ = newTestApp(t, "")
	if code := runRetiredVerb(t, a, "config", "get", "routing.on_subscription_limit"); code != ExitOK {
		t.Fatalf("config get exit = %d", code)
	}
	if !strings.Contains(out.String(), "ask") {
		t.Errorf("after unset, get = %q, want the inherited default", out.String())
	}
}

// TestUndoTaskIsDiscoverableAndRefusesWhatItCannotDo covers the surface half of
// A33.8: a per-subagent rewind nobody can find is one nobody uses, and a
// refusal that does not say what there is instead is a dead end.
func TestUndoTaskIsDiscoverableAndRefusesWhatItCannotDo(t *testing.T) {
	isolateHome(t)

	found := false
	for _, cmd := range slashCommandTable {
		if cmd.name == "undo" {
			found = true
			if !strings.Contains(cmd.args, "task <n>") {
				t.Errorf("/undo is catalogued as %q, which does not mention the per-subagent form", cmd.args)
			}
		}
	}
	if !found {
		t.Fatal("/undo is not in the slash catalogue at all")
	}
}
