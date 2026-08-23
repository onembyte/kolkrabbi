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
	"github.com/onembyte/kolkrabbi/internal/paths"
	"github.com/onembyte/kolkrabbi/internal/selfupdate"
)

// newTestApp builds an app whose streams are buffers, so a whole kolk
// invocation can be run and asserted on in-process.
func newTestApp(stdin string) (*app, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	a := newApp()
	a.stdout = &out
	a.stderr = &errOut
	a.in = bufio.NewReader(strings.NewReader(stdin))
	return a, &out, &errOut
}

// isolateHome points kolk at a temp directory so tests never read or write the
// developer's real state, and clears the env key so a key in the shell running
// the tests cannot change the outcome.
//
// It sets the KOLK_*_DIR overrides rather than $HOME: those are the one thing
// that means the same on every platform, so the tests do not quietly depend on
// the unix layout.
func isolateHome(t *testing.T) paths.Dirs {
	t.Helper()
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
	t.Setenv("OPENROUTER_BASE_URL", "")
	t.Setenv("CI", "")
	return d
}

func TestHelpDocumentsEveryCommandAndFlag(t *testing.T) {
	a, out, _ := newTestApp("")
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
		a, out, _ := newTestApp("")
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
	a, out, errOut := newTestApp("")
	calls := 0
	a.update = func(context.Context) (selfupdate.Result, error) {
		calls++
		return selfupdate.Result{
			Current: "1.0.0", Latest: "1.2.3", Updated: true, Path: "/usr/local/bin/kolk",
		}, nil
	}

	if code := a.main(context.Background(), []string{"update"}); code != ExitOK {
		t.Fatalf("kolk update exit = %d, stderr %q", code, errOut.String())
	}
	if calls != 1 {
		t.Fatalf("updater calls = %d, want 1", calls)
	}
	for _, want := range []string{"updated kolk 1.0.0 → 1.2.3", "/usr/local/bin/kolk"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("update output omitted %q: %q", want, out.String())
		}
	}
	for _, dir := range []string{d.Config, d.Data, d.Cache} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("keyless update created state at %s: %v", dir, err)
		}
	}
}

func TestTopLevelUpdateRejectsArgumentsBeforeCallingUpdater(t *testing.T) {
	a, _, errOut := newTestApp("")
	calls := 0
	a.update = func(context.Context) (selfupdate.Result, error) {
		calls++
		return selfupdate.Result{}, nil
	}
	if code := a.main(context.Background(), []string{"update", "now"}); code != ExitUsage {
		t.Fatalf("update with argument exit = %d, want %d", code, ExitUsage)
	}
	if calls != 0 || !strings.Contains(errOut.String(), "usage: kolk update") {
		t.Fatalf("calls = %d, stderr = %q", calls, errOut.String())
	}
}

func TestTopLevelUpdateReportsUnchangedFailureAndWarning(t *testing.T) {
	t.Run("unchanged", func(t *testing.T) {
		a, out, _ := newTestApp("")
		a.update = func(context.Context) (selfupdate.Result, error) {
			return selfupdate.Result{Current: "1.2.3", Latest: "1.2.3"}, nil
		}
		if code := a.main(context.Background(), []string{"update"}); code != ExitOK {
			t.Fatalf("exit = %d", code)
		}
		if !strings.Contains(out.String(), "kolk 1.2.3 is already current") {
			t.Fatalf("unchanged output = %q", out.String())
		}
	})

	t.Run("failure", func(t *testing.T) {
		a, _, errOut := newTestApp("")
		a.update = func(context.Context) (selfupdate.Result, error) {
			return selfupdate.Result{}, errors.New("release unavailable")
		}
		if code := a.main(context.Background(), []string{"update"}); code != ExitError {
			t.Fatalf("exit = %d, want %d", code, ExitError)
		}
		if !strings.Contains(errOut.String(), "release unavailable") {
			t.Fatalf("failure stderr = %q", errOut.String())
		}
	})

	t.Run("durability warning", func(t *testing.T) {
		a, out, errOut := newTestApp("")
		a.update = func(context.Context) (selfupdate.Result, error) {
			return selfupdate.Result{
				Current: "1.0.0", Latest: "1.2.3", Updated: true,
				Path: "/bin/kolk", Warning: "directory sync refused",
			}, nil
		}
		if code := a.main(context.Background(), []string{"update"}); code != ExitOK {
			t.Fatalf("exit = %d", code)
		}
		if !strings.Contains(out.String(), "updated kolk") || !strings.Contains(errOut.String(), "warning: directory sync refused") {
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
		if len(c.name) > 8 {
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
	a, out, errOut := newTestApp("")

	code := a.main(context.Background(), nil)
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d", code, ExitUsage)
	}
	const want = "kolk needs an API key before it can use models.\n" +
		"Add one:  kolk key <API_KEY>\n" +
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
		a, _, errOut := newTestApp("")
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

	a, out, _ := newTestApp("")
	if code := a.main(context.Background(), []string{"sessions"}); code != ExitOK {
		t.Fatalf("kolk sessions exit = %d", code)
	}
	if !strings.Contains(out.String(), "no sessions yet") {
		t.Errorf("kolk sessions on a fresh machine printed %q", out.String())
	}

	a, out, _ = newTestApp("")
	if code := a.main(context.Background(), []string{"stats"}); code != ExitOK {
		t.Fatalf("kolk stats exit = %d", code)
	}
	if !strings.Contains(out.String(), "nothing ever leaves this machine") {
		t.Errorf("kolk stats must state the local-only promise, got %q", out.String())
	}
}

func TestConfigSettingsRoundTripWithoutACredentialField(t *testing.T) {
	isolateHome(t)

	a, _, _ := newTestApp("")
	if code := a.main(context.Background(), []string{"config", "set-tier", "quick", "google/gemini-2.5-flash"}); code != ExitOK {
		t.Fatalf("config set-tier exit = %d", code)
	}

	a, out, _ := newTestApp("")
	if code := a.main(context.Background(), []string{"config", "show"}); code != ExitOK {
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

	a, _, errOut := newTestApp("")
	if code := a.main(context.Background(), []string{"config", "set-model", "new/model"}); code != ExitOK {
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

	a, _, errOut = newTestApp("")
	if code := a.main(context.Background(), []string{"config", "set-base-url", "https://second.test"}); code != ExitOK {
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

	a, _, _ := newTestApp("")
	if code := a.main(context.Background(), []string{"config", "set-tier", "medium", "some/model"}); code != ExitUsage {
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
	a, _, _ := newTestApp("")
	if code := a.main(context.Background(), []string{"config", "set-tier", "medium", "some/model"}); code != ExitUsage {
		t.Errorf("set-tier medium exit = %d, want %d", code, ExitUsage)
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
	a, out, _ := newTestApp("")
	if code := a.main(context.Background(), []string{"help", "config"}); code != ExitOK {
		t.Fatalf("kolk help config exit = %d", code)
	}
	got := out.String()
	if !strings.Contains(got, "usage: kolk config") {
		t.Errorf("kolk help config did not print a usage line:\n%s", got)
	}
	if !strings.Contains(got, "set-tier") {
		t.Errorf("kolk help config did not print the argument grammar:\n%s", got)
	}
}

func TestHelpForAnUnknownCommandIsAUsageError(t *testing.T) {
	a, _, _ := newTestApp("")
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
	a, _, errOut := newTestApp("")
	if code := a.main(context.Background(), []string{"config", "set-everything"}); code != ExitUsage {
		t.Fatalf("exit = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(errOut.String(), "usage: kolk config") {
		t.Errorf("bad subcommand did not print generated usage: %q", errOut.String())
	}
}

// The directories are a decision with consequences: settings may be symlinked
// into dotfiles, while a credential must remain in private state.
func TestStateAndConfigAreSeparateOnDisk(t *testing.T) {
	d := isolateHome(t)

	a, _, _ := newTestApp("")
	if code := a.main(context.Background(), []string{"config", "set-model", "openrouter/auto"}); code != ExitOK {
		t.Fatalf("config set-model exit = %d", code)
	}
	if _, err := os.Stat(d.ConfigFile()); err != nil {
		t.Errorf("config did not land in the config directory: %v", err)
	}

	const mistralKey = "0123456789abcdef0123456789abcdef"
	a, _, _ = newTestApp(mistralKey + "\n")
	if code := a.main(context.Background(), []string{"key", "mistral", "-"}); code != ExitOK {
		t.Fatalf("kolk key exit = %d", code)
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
func TestHelpAndVersionNeedNoDirectories(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv(paths.EnvConfigDir, "")
	t.Setenv(paths.EnvDataDir, "")
	t.Setenv(paths.EnvCacheDir, "")

	for _, verb := range []string{"help", "version"} {
		a, out, _ := newTestApp("")
		if code := a.main(context.Background(), []string{verb}); code != ExitOK {
			t.Errorf("kolk %s exit = %d with no resolvable home directory", verb, code)
		}
		if out.Len() == 0 {
			t.Errorf("kolk %s printed nothing", verb)
		}
	}
}
