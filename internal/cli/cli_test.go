package cli

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/paths"
)

// newTestApp builds an app whose streams are buffers, so a whole kolk
// invocation can be run and asserted on in-process.
func newTestApp(stdin string) (*app, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	return &app{stdout: &out, stderr: &errOut, in: bufio.NewReader(strings.NewReader(stdin))}, &out, &errOut
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

func TestFirstRunWithoutAKeyExplainsWhatToType(t *testing.T) {
	isolateHome(t)
	a, _, errOut := newTestApp("")

	code := a.main(context.Background(), []string{"-p", "hello"})
	if code != ExitError {
		t.Fatalf("exit = %d, want %d", code, ExitError)
	}
	got := errOut.String()
	if !strings.Contains(got, "kolk config set-key") {
		t.Errorf("first-run failure must name the command that fixes it, got:\n%s", got)
	}
	if !strings.Contains(got, "OPENROUTER_API_KEY") {
		t.Errorf("first-run failure must mention the env var, got:\n%s", got)
	}
	if strings.Contains(got, "error: No OpenRouter") {
		t.Errorf("guided errors print their own headline, not the error: prefix; got:\n%s", got)
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

func TestConfigRoundTripsThroughDisk(t *testing.T) {
	isolateHome(t)

	a, _, _ := newTestApp("")
	if code := a.main(context.Background(), []string{"config", "set-key", "sk-or-v1-abcdef0123456789"}); code != ExitOK {
		t.Fatalf("config set-key exit = %d", code)
	}
	a, _, _ = newTestApp("")
	if code := a.main(context.Background(), []string{"config", "set-tier", "quick", "google/gemini-2.5-flash"}); code != ExitOK {
		t.Fatalf("config set-tier exit = %d", code)
	}

	a, out, _ := newTestApp("")
	if code := a.main(context.Background(), []string{"config", "show"}); code != ExitOK {
		t.Fatalf("config show exit = %d", code)
	}
	got := out.String()
	if strings.Contains(got, "sk-or-v1-abcdef0123456789") {
		t.Errorf("config show leaked the whole key:\n%s", got)
	}
	if !strings.Contains(got, "sk-or-…6789") {
		t.Errorf("config show did not mask the key recognisably:\n%s", got)
	}
	if !strings.Contains(got, "google/gemini-2.5-flash") {
		t.Errorf("config show lost the saved tier:\n%s", got)
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

// The directories are a decision with consequences: a key that lands in a
// config directory someone symlinks into a dotfiles repo is a published key.
func TestStateAndConfigAreSeparateOnDisk(t *testing.T) {
	d := isolateHome(t)

	a, _, _ := newTestApp("")
	if code := a.main(context.Background(), []string{"config", "set-key", "sk-or-v1-locationtest0000"}); code != ExitOK {
		t.Fatalf("config set-key exit = %d", code)
	}
	if _, err := os.Stat(d.ConfigFile()); err != nil {
		t.Errorf("config did not land in the config directory: %v", err)
	}

	a, _, _ = newTestApp("")
	if code := a.main(context.Background(), []string{"sessions"}); code != ExitOK {
		t.Fatalf("kolk sessions exit = %d", code)
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
