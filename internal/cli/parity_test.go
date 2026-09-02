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
	"sessions":  "plural because it lists; `sess` was judged worse to read",
	"uninstall": "the one verb people look for while frustrated; a short alias nobody guesses is worse than nine letters",
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

// TestOutsideSessionSurfaceIsClosed is the enforcement of the owner's decision
// of 2026-09-02 (docs/plan/09, "the outside-session surface is closed").
//
// The session is the product. A verb out here has to be something a session
// cannot do, and exactly four are: `sessions` lists what you consult before
// opening one, `serve` hosts sessions rather than running inside one,
// `uninstall` deletes the binary and the state a session is writing, and `help`
// is the front door.
//
// **Adding a fifth fails this test on purpose.** The failure mode being guarded
// is not a bad command — it is a surface that grows one reasonable verb at a
// time until the session is no longer the product. If a new one is genuinely
// needed, the plan says to put it to the owner twice before this list is
// touched.
func TestOutsideSessionSurfaceIsClosed(t *testing.T) {
	want := []string{"sessions", "serve", "uninstall", "help"}
	var got []string
	for _, cmd := range commandTable() {
		got = append(got, cmd.name)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("outside-session verbs = %v, want exactly %v.\nEverything else is a slash command; see the amendment in docs/plan/09-command-surface.md before changing this.", got, want)
	}
}

// TestRetiredVerbsAreGoneFromTheCLIAndPresentInTheSession is the other half:
// each command that stopped being a verb is still reachable where it now
// lives, so the removal moved the door rather than the room.
func TestRetiredVerbsAreGoneFromTheCLIAndPresentInTheSession(t *testing.T) {
	retired := []string{"key", "model", "effort", "mode", "config", "models", "plans",
		"pmodels", "localia", "update", "stats", "dash", "devices", "version", "doctor"}
	slash := map[string]bool{}
	for _, sc := range slashCommandTable {
		slash[sc.name] = true
	}
	for _, name := range retired {
		if lookupCommand(name) != nil {
			t.Errorf("%q is still an outside-session verb; the plan says it is a slash command now", name)
		}
		canonical := name
		if canonical == "models" {
			canonical = "model" // one command, listing when bare
		}
		if !slash[canonical] {
			t.Errorf("%q was removed from the CLI and has no /%s to move to — that is a capability deleted, not relocated", name, canonical)
		}
	}
	// `completion` is the one genuine deletion: it generated a shell script
	// for a surface that no longer needs completing.
	if lookupCommand("completion") != nil || slash["completion"] {
		t.Error("completion came back; it was deleted, not moved")
	}
}

// TestModelAndEffortWorkInSession replaces the top-level-verb test of the same
// intent: `kolk model` and `kolk effort` are gone, and the behaviour they
// covered now belongs to /model and /effort.
func TestModelAndEffortWorkInSession(t *testing.T) {
	a, ag, out := replFixture(t, "")
	seedModelCatalog(t, a.dirs)

	// 1. /model bare lists what can be chosen.
	a.slash(context.Background(), ag, "/model")
	if got := out.String(); !strings.Contains(got, "ctx") {
		t.Errorf("/model did not list the catalog: %q", got)
	}

	// 2. /model <alias> switches this session.
	out.Reset()
	a.slash(context.Background(), ag, "/model sonnet")
	if got := out.String(); !strings.Contains(got, "anthropic/claude-3-7-sonnet") {
		t.Errorf("/model sonnet = %q", got)
	}

	// 3. /effort bare shows it; 4. /effort <level> sets it.
	out.Reset()
	a.slash(context.Background(), ag, "/effort")
	if got := out.String(); !strings.Contains(got, "effort") {
		t.Errorf("/effort = %q", got)
	}
	out.Reset()
	a.slash(context.Background(), ag, "/effort high")
	if got := out.String(); !strings.Contains(got, "high") {
		t.Errorf("/effort high = %q", got)
	}
	if ag.Effort != "high" {
		t.Errorf("session effort = %q, want high", ag.Effort)
	}
}

// The two tests that used to live here — TestEverySlashCommandIsAccountedFor
// and TestTheSessionOnlyListDoesNotRot — asked every slash command to justify
// having no `kolk <verb>` twin, against a hand-kept `sessionOnly` list of
// reasons.
//
// The amendment of 2026-09-02 inverts that question. A slash command needs no
// CLI twin and never did; it is an outside-session verb that now has to
// justify itself, which TestOutsideSessionSurfaceIsClosed does by name. Keeping
// the old list would mean maintaining a reason for all thirty-six of them, all
// of which would read "because the session is the product".

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
