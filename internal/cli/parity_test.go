package cli

import (
	"context"
	"github.com/onembyte/kolkrabbi/internal/paths"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// longVerbs are the shipped commands that break the "≤ 6 letters" rule in
// docs/plan/09-command-surface.md §1.2.
//
// They are listed rather than fixed because they are published: `kolk sessions`
// and `kolk version` are in v1.2.1 and in the installer's documentation, so
// renaming them is a product decision with a deprecation cost, not a tidy-up.
// The list exists so the violation is visible and bounded — a new long verb
// fails the test, and shortening one of these means deleting its line here.
var longVerbs = map[string]string{
	"completion": "generates a shell script; typed once per machine, never in a session",
	"localia":    "the local-model planner; named for the feature, not the keystrokes",
	"pmodels":    "plan models; `pmodel` reads as a typo of `model`",
	"devices":    "plural because it lists, like `sessions`; `device` reads as a flag for one",
	"sessions":   "plural because it lists; `sess` was judged worse to read",
	"version":    "what every other CLI calls it, and muscle memory beats the rule",
	"uninstall":  "the one verb people look for while frustrated; a short alias nobody guesses is worse than nine letters",
}

// TestCommandNameLengthGuardrail enforces the naming rules in
// docs/plan/09-command-surface.md §1.2 against the command table itself.
//
// It used to check a hardcoded list of thirteen names, which meant it asserted
// `len("key") > 6` — decidable when it was written and unable to fail
// afterwards. It also named `login`, `doctor` and `exit`, none of which are
// commands: two are planned and one is a REPL-only word. A guardrail that
// cannot observe what it guards is documentation with a test's name on it.
func TestCommandNameLengthGuardrail(t *testing.T) {
	for _, cmd := range commandTable() {
		name := cmd.name
		if strings.ToLower(name) != name {
			t.Errorf("verb %q must be all lowercase", name)
		}
		if strings.ContainsAny(name, " -_\t\n") {
			t.Errorf("verb %q must be a single word without hyphens or spaces", name)
		}
		if len(name) > 6 && longVerbs[name] == "" {
			t.Errorf("verb %q is %d characters; the rule is 6. Shorten it, or add it to longVerbs with the reason it earns an exception.",
				name, len(name))
		}
	}
}

// TestTheLongVerbListDoesNotRot checks the exemptions against the table.
//
// An exemption for a command that no longer exists is a claim nobody will
// re-read, and it quietly widens the rule for a name someone might reuse.
func TestTheLongVerbListDoesNotRot(t *testing.T) {
	real := make(map[string]bool)
	for _, cmd := range commandTable() {
		real[cmd.name] = true
	}
	for name, reason := range longVerbs {
		if !real[name] {
			t.Errorf("longVerbs exempts %q, which is not a command", name)
		}
		if len(name) <= 6 {
			t.Errorf("%q is within the rule and needs no exemption", name)
		}
		if reason == "" {
			t.Errorf("%q is exempt with no reason given", name)
		}
	}
}

// TestTopLevelAndSlashParity verifies that every top-level CLI verb has an
// identical slash twin inside the REPL command registry.
func TestTopLevelAndSlashParity(t *testing.T) {
	slashMap := make(map[string]bool)
	for _, sc := range slashCommandTable {
		trimmed := strings.TrimPrefix(sc.name, "/")
		slashMap[trimmed] = true
	}

	// Commands that are batch/daemon only and do not apply inside an active session:
	batchOnly := map[string]bool{
		"serve":      true, // daemon/stdio server
		"completion": true, // shell script generator
		// uninstall removes the running binary and the session's own state.
		// Offering it mid-session would mean deleting the sessions file being
		// written to and the executable currently reading the keyboard.
		"uninstall": true,
	}

	for _, cmd := range commandTable() {
		if batchOnly[cmd.name] {
			continue
		}
		canonical := cmd.name
		if canonical == "models" {
			canonical = "model"
		}
		if canonical == "sessions" {
			continue // session management
		}
		if !slashMap[canonical] {
			t.Errorf("CLI command %q has no slash twin /%s in REPL slashCommands table", cmd.name, canonical)
		}
	}
}

// TestModelAndEffortTopLevelCommandsWork verifies that `kolk model` and
// `kolk effort` operate as first-class CLI verbs.
func TestModelAndEffortTopLevelCommandsWork(t *testing.T) {
	d := isolateHome(t)

	// `kolk model` lists the catalog, which means fetching it. Without a
	// catalog to fetch this test reached the real provider — quietly, until
	// isolateHome started pointing stray calls at a closed port. Seeding the
	// cache keeps the test about the command rather than about the network.
	seedModelCatalog(t, d)

	// 1. kolk model bare lists catalog
	a, out, errOut := newTestApp(t, "test-key")
	if code := a.main(context.Background(), []string{"model"}); code != ExitOK {
		t.Fatalf("kolk model exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "ctx") {
		t.Errorf("kolk model output missing catalog: %q", out.String())
	}

	// 2. kolk model <alias> sets model
	a, out, errOut = newTestApp(t, "test-key")
	if code := a.main(context.Background(), []string{"model", "sonnet"}); code != ExitOK {
		t.Fatalf("kolk model sonnet exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "anthropic/claude-3-7-sonnet") {
		t.Errorf("kolk model sonnet output = %q", out.String())
	}

	// 3. kolk effort bare shows effort
	a, out, errOut = newTestApp(t, "")
	if code := a.main(context.Background(), []string{"effort"}); code != ExitOK {
		t.Fatalf("kolk effort exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "effort") {
		t.Errorf("kolk effort output = %q", out.String())
	}

	// 4. kolk effort <level> sets effort
	a, out, errOut = newTestApp(t, "")
	if code := a.main(context.Background(), []string{"effort", "high"}); code != ExitOK {
		t.Fatalf("kolk effort high exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "high") {
		t.Errorf("kolk effort high output = %q", out.String())
	}
}

// sessionOnly are slash commands with no `kolk <verb>` twin, and the reason.
//
// docs/plan/09-command-surface.md §7 states parity as an equivalence, but only
// one direction was ever tested: every CLI verb needed a slash twin, and a
// slash-only command was never questioned. So /diff, /undo and /plan arrived
// with no twin and nothing noticed.
//
// Most of these are session-only for a real reason — they act on a live
// conversation that a one-shot process does not have. Writing the reason down
// is the point: it turns "nobody built the twin" into "there is nothing for the
// twin to act on", which are different, and only one of them is a gap.
var sessionOnly = map[string]string{
	"ask":          "sets the permission tier of the running session",
	"auto-approve": "sets the permission tier of the running session",
	"full-auto":    "sets the permission tier of the running session",
	"permissions":  "shows and edits the running session's tier and rules",
	"plan":         "puts the running session into read-only planning",
	"commit":       "drafts through the running session's fast lane, which a one-shot process has no model wired for",
	"pr":           "drafts through the running session's fast lane, like /commit",
	"compact":      "shrinks the conversation this process is holding",
	"remember":     "appends what the session just learned",
	"rate":         "rates the turn that just happened",
	"changes":      "lists what this session changed",
	"diff":         "shows what this session changed; a fresh process has no session to diff",
	"undo":         "takes back this session's last turn",
	"rewind":       "restores this session's last turn's files",
	"session":      "identifies the running session",
	"new":          "starts a fresh session inside the running process",
	"clear":        "alias for /new",
	"exit":         "leaves the REPL; a one-shot process has already left",
	"quit":         "alias for /exit",
	"plogin":       "picks a plan to log into, interactively",
}

// TestEverySlashCommandIsAccountedFor closes the other half of the parity rule.
func TestEverySlashCommandIsAccountedFor(t *testing.T) {
	cliVerbs := map[string]bool{}
	for _, cmd := range commandTable() {
		cliVerbs[cmd.name] = true
	}

	for _, sc := range slashCommandTable {
		name := strings.TrimPrefix(sc.name, "/")
		if cliVerbs[name] {
			continue
		}
		if reason := sessionOnly[name]; reason == "" {
			t.Errorf("/%s has no `kolk %s` twin and no reason recorded — add the twin, or add it to sessionOnly saying what it acts on that a one-shot process lacks", name, name)
		}
	}
}

// TestTheSessionOnlyListDoesNotRot keeps its entries real.
func TestTheSessionOnlyListDoesNotRot(t *testing.T) {
	slashNames := map[string]bool{}
	for _, sc := range slashCommandTable {
		slashNames[strings.TrimPrefix(sc.name, "/")] = true
	}
	cliVerbs := map[string]bool{}
	for _, cmd := range commandTable() {
		cliVerbs[cmd.name] = true
	}

	for name := range sessionOnly {
		if !slashNames[name] {
			t.Errorf("sessionOnly names /%s, which is not a slash command", name)
		}
		if cliVerbs[name] {
			t.Errorf("/%s has a CLI twin now and no longer needs to be listed as session-only", name)
		}
	}
}

// seedModelCatalog writes a cached catalog so a command that lists models has
// something to list without asking a provider.
func seedModelCatalog(t *testing.T, d paths.Dirs) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(d.CatalogFile()), 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{"fetched_at":"2099-01-01T00:00:00Z","models":[` +
		`{"id":"anthropic/claude-sonnet-4","name":"Sonnet","context_length":200000},` +
		`{"id":"vendor/free-model","name":"Free","context_length":8192}]}`
	if err := os.WriteFile(d.CatalogFile(), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
