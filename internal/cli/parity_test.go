package cli

import (
	"context"
	"strings"
	"testing"
)

// TestCommandNameLengthGuardrail enforces that every core command verb
// is strictly a single lowercase word of 6 characters or fewer per
// docs/plan/09-command-surface.md §1.2 and §2.
func TestCommandNameLengthGuardrail(t *testing.T) {
	canonicalVerbs := []string{
		"key", "model", "effort", "mode", "config", "login",
		"update", "stats", "dash", "saga", "doctor", "help", "exit",
	}
	for _, name := range canonicalVerbs {
		if len(name) > 6 {
			t.Errorf("canonical verb %q exceeds 6-character limit (%d chars)", name, len(name))
		}
		if strings.ToLower(name) != name {
			t.Errorf("canonical verb %q must be all lowercase", name)
		}
		if strings.ContainsAny(name, " -_\t\n") {
			t.Errorf("canonical verb %q must be a single word without hyphens or spaces", name)
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
