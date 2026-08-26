package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/paths"
	"github.com/onembyte/kolkrabbi/internal/tools"
)

func TestPermissionsShowsAllThreeAndMarksTheCurrentOne(t *testing.T) {
	isolateConnectorState(t)
	a, ag, out := replFixture(t, "")
	ag.Permission = engine.PermissionAsk

	if a.slash(context.Background(), ag, "/permissions") {
		t.Fatal("/permissions must not exit the session")
	}

	got := out.String()
	for _, tier := range []string{"ask", "auto-approve", "full-auto"} {
		if !strings.Contains(got, tier) {
			t.Fatalf("output = %q, want every tier listed", got)
		}
	}
	// The current one has to be identifiable at a glance, or the list is just
	// documentation.
	if !strings.Contains(got, "→ ask") && !strings.Contains(got, "* ask") {
		t.Fatalf("output = %q, want the active tier marked", got)
	}
	// Each tier must say what it actually does, including what it still refuses.
	if !strings.Contains(got, "refus") {
		t.Fatalf("output = %q, want the floor mentioned", got)
	}
}

func TestPermissionsSelectsATier(t *testing.T) {
	isolateConnectorState(t)
	a, ag, out := replFixture(t, "")

	if a.slash(context.Background(), ag, "/permissions full-auto") {
		t.Fatal("/permissions must not exit the session")
	}
	if ag.Permission != engine.PermissionFullAuto {
		t.Fatalf("permission = %q", ag.Permission)
	}
	if !strings.Contains(out.String(), "full-auto") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestEachTierHasItsOwnCommand(t *testing.T) {
	isolateConnectorState(t)
	for command, want := range map[string]engine.Permission{
		"/ask":          engine.PermissionAsk,
		"/auto-approve": engine.PermissionAutoApprove,
		"/full-auto":    engine.PermissionFullAuto,
	} {
		a, ag, out := replFixture(t, "")
		if a.slash(context.Background(), ag, command) {
			t.Fatalf("%s must not exit the session", command)
		}
		if ag.Permission != want {
			t.Fatalf("%s set %q, want %q", command, ag.Permission, want)
		}
		if !strings.Contains(out.String(), string(want)) {
			t.Fatalf("%s said %q", command, out.String())
		}
	}
}

func TestFullAutoSaysWhatItStillRefuses(t *testing.T) {
	isolateConnectorState(t)
	a, ag, out := replFixture(t, "")

	a.slash(context.Background(), ag, "/full-auto")

	// Switching to the most permissive tier is the moment to say that it is
	// not unlimited.
	got := strings.ToLower(out.String())
	if !strings.Contains(got, "still") || !strings.Contains(got, "refus") {
		t.Fatalf("output = %q, want the floor stated when entering full-auto", out.String())
	}
}

func TestUnknownTierIsRejectedWithTheChoices(t *testing.T) {
	isolateConnectorState(t)
	a, ag, out := replFixture(t, "")
	var errOut strings.Builder
	a.stderr = &errOut

	a.slash(context.Background(), ag, "/permissions yolo")

	if ag.Permission == "yolo" {
		t.Fatal("an unknown tier was accepted")
	}
	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "ask") || !strings.Contains(combined, "full-auto") {
		t.Fatalf("output = %q, want the valid choices listed", combined)
	}
}

func TestYoloIsGone(t *testing.T) {
	isolateConnectorState(t)
	a, ag, out := replFixture(t, "")

	a.slash(context.Background(), ag, "/yolo")

	if !strings.Contains(out.String(), "unknown command") {
		t.Fatalf("output = %q, want /yolo to no longer exist", out.String())
	}
}

// ruleFixture gives the app a config directory of its own so a test can add
// rules without touching the developer's real permission file.
func ruleFixture(t *testing.T) (*app, *engine.Agent, *bytes.Buffer) {
	t.Helper()
	isolateConnectorState(t)
	a, ag, out := replFixture(t, "")
	a.dirs = paths.Dirs{Config: t.TempDir(), Data: t.TempDir(), Cache: t.TempDir()}
	ag.Root = "/p"
	ag.Permission = engine.PermissionAsk
	return a, ag, out
}

func TestAddingARuleTakesEffectImmediately(t *testing.T) {
	a, ag, _ := ruleFixture(t)

	a.slash(context.Background(), ag, "/permissions allow bash(git *)")

	// The point of writing a rule is not to have to restart.
	verdict, _ := ag.Judge(tools.Request{Tool: "bash", Command: "git status"})
	if verdict != engine.VerdictAllow {
		t.Fatalf("the rule did not reach the running session: verdict = %v", verdict)
	}
}

func TestAProjectRuleIsWrittenWhereItWillBeFoundAgain(t *testing.T) {
	a, ag, _ := ruleFixture(t)

	a.slash(context.Background(), ag, "/permissions allow bash(go test *)")

	stored, err := config.LoadPermissions(config.PermissionsFile(a.dirs.Config))
	if err != nil {
		t.Fatalf("loading what was written: %v", err)
	}
	if got := stored.For("/p"); len(got) != 1 || got[0] != "allow bash(go test *)" {
		t.Fatalf("stored %v for this project", got)
	}
	// It is this project's rule, not everyone's.
	if len(stored.Always) != 0 {
		t.Fatalf("stored %v globally, want nothing", stored.Always)
	}
}

func TestScopeAlwaysAppliesEverywhereAndSessionIsNotWritten(t *testing.T) {
	a, ag, _ := ruleFixture(t)

	a.slash(context.Background(), ag, "/permissions deny write(*.pem) always")
	a.slash(context.Background(), ag, "/permissions allow bash(ls *) session")

	stored, _ := config.LoadPermissions(config.PermissionsFile(a.dirs.Config))
	if len(stored.Always) != 1 || stored.Always[0] != "deny write(*.pem)" {
		t.Fatalf("always-scope stored %v", stored.Always)
	}
	// A session rule that outlives the session is a rule nobody consented to.
	if len(stored.For("/p")) != 1 {
		t.Fatalf("a session rule was written to disk: %v", stored.For("/p"))
	}
	// It still has to work for the rest of this session.
	if verdict, _ := ag.Judge(tools.Request{Tool: "bash", Command: "ls -la"}); verdict != engine.VerdictAllow {
		t.Fatalf("the session rule is not in effect: %v", verdict)
	}
}

func TestTheRulesInEffectAreListedWithTheirScope(t *testing.T) {
	a, ag, out := ruleFixture(t)
	a.slash(context.Background(), ag, "/permissions deny write(*.pem) always")
	a.slash(context.Background(), ag, "/permissions allow bash(git *)")
	a.slash(context.Background(), ag, "/permissions ask write(*) session")
	out.Reset()

	a.slash(context.Background(), ag, "/permissions")

	got := out.String()
	for _, want := range []string{"deny write(*.pem)", "allow bash(git *)", "ask write(*)", "always", "project", "session"} {
		if !strings.Contains(got, want) {
			t.Fatalf("listing = %q, want it to mention %q", got, want)
		}
	}
}

func TestARuleCanBeForgottenByItsNumber(t *testing.T) {
	a, ag, out := ruleFixture(t)
	a.slash(context.Background(), ag, "/permissions allow bash(git *)")
	a.slash(context.Background(), ag, "/permissions allow bash(rm *)")
	out.Reset()

	a.slash(context.Background(), ag, "/permissions forget 2")

	if verdict, _ := ag.Judge(tools.Request{Tool: "bash", Command: "rm ./x"}); verdict == engine.VerdictAllow {
		t.Fatal("the forgotten rule is still in effect")
	}
	if verdict, _ := ag.Judge(tools.Request{Tool: "bash", Command: "git status"}); verdict != engine.VerdictAllow {
		t.Fatal("forgetting one rule removed another")
	}
	stored, _ := config.LoadPermissions(config.PermissionsFile(a.dirs.Config))
	if got := stored.For("/p"); len(got) != 1 {
		t.Fatalf("disk still holds %v", got)
	}
}

func TestARuleThatDoesNotParseIsRefusedAndNotStored(t *testing.T) {
	a, ag, out := ruleFixture(t)
	var errOut bytes.Buffer
	a.stderr = &errOut

	a.slash(context.Background(), ag, "/permissions allow bash(")

	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "allow bash(") {
		t.Fatalf("output = %q, want the bad line quoted back", combined)
	}
	stored, _ := config.LoadPermissions(config.PermissionsFile(a.dirs.Config))
	if len(stored.For("/p")) != 0 {
		t.Fatalf("a rule that does not parse was stored: %v", stored.For("/p"))
	}
}
