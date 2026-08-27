package cli

import (
	"context"
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
	"sessions":   "plural because it lists; `sess` was judged worse to read",
	"version":    "what every other CLI calls it, and muscle memory beats the rule",
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
	isolateHome(t)

	// 1. kolk model bare lists catalog
	a, out, errOut := newTestApp("test-key")
	if code := a.main(context.Background(), []string{"model"}); code != ExitOK {
		t.Fatalf("kolk model exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "ctx") {
		t.Errorf("kolk model output missing catalog: %q", out.String())
	}

	// 2. kolk model <alias> sets model
	a, out, errOut = newTestApp("test-key")
	if code := a.main(context.Background(), []string{"model", "sonnet"}); code != ExitOK {
		t.Fatalf("kolk model sonnet exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "anthropic/claude-3-7-sonnet") {
		t.Errorf("kolk model sonnet output = %q", out.String())
	}

	// 3. kolk effort bare shows effort
	a, out, errOut = newTestApp("")
	if code := a.main(context.Background(), []string{"effort"}); code != ExitOK {
		t.Fatalf("kolk effort exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "effort") {
		t.Errorf("kolk effort output = %q", out.String())
	}

	// 4. kolk effort <level> sets effort
	a, out, errOut = newTestApp("")
	if code := a.main(context.Background(), []string{"effort", "high"}); code != ExitOK {
		t.Fatalf("kolk effort high exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "high") {
		t.Errorf("kolk effort high output = %q", out.String())
	}
}
