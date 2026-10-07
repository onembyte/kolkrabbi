package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/paths"
	"github.com/onembyte/kolkrabbi/internal/tools"
)

func planFixture(t *testing.T) (*app, *engine.Agent) {
	t.Helper()
	isolateConnectorState(t)
	a, ag, _ := replFixture(t, "")
	a.dirs = paths.Dirs{Config: t.TempDir(), Data: t.TempDir(), Cache: t.TempDir()}
	ag.Root = "/p"
	ag.Permission = engine.PermissionFullAuto
	return a, ag
}

func TestPlanModeRefusesEveryWriteAndCommandEvenInFullAuto(t *testing.T) {
	a, ag := planFixture(t)

	a.slash(context.Background(), ag, "/plan")

	for _, request := range []tools.Request{
		{Tool: "write_file", Path: "/p/a.go", Display: "a.go"},
		{Tool: "edit_file", Path: "/p/a.go", Display: "a.go"},
		{Tool: "bash", Command: "go test ./..."},
	} {
		if verdict, reason := ag.Judge(request); verdict != engine.VerdictDeny {
			t.Fatalf("%s = %v (%s), want a refusal in plan mode", request.Tool, verdict, reason)
		}
	}
}

func TestPlanModeStillReads(t *testing.T) {
	a, ag := planFixture(t)

	a.slash(context.Background(), ag, "/plan")

	// Read-only exploration is the whole point; a mode that cannot read is
	// not a planning mode, it is an off switch.
	for _, tool := range []string{"read_file", "list_dir"} {
		if verdict, _ := ag.Judge(tools.Request{Tool: tool, Path: "/p/a.go", Display: "a.go"}); verdict != engine.VerdictAllow {
			t.Fatalf("%s was refused in plan mode", tool)
		}
	}
}

func TestLeavingPlanModeRestoresWhatItTookAway(t *testing.T) {
	a, ag := planFixture(t)
	a.slash(context.Background(), ag, "/permissions allow bash(git *) session")
	before := len(ag.Rules)

	a.slash(context.Background(), ag, "/plan")
	a.slash(context.Background(), ag, "/plan off")

	if verdict, _ := ag.Judge(tools.Request{Tool: "write_file", Path: "/p/a.go", Display: "a.go"}); verdict != engine.VerdictAllow {
		t.Fatal("leaving plan mode did not restore writing")
	}
	// Leaving drops the rules plan mode added, and nothing else.
	if got := len(ag.Rules); got != before {
		t.Fatalf("rules = %d, want the %d that were there before plan mode", got, before)
	}
}

func TestPermissionsExplainsWhyPlanModeIsRefusing(t *testing.T) {
	a, ag := planFixture(t)
	a.slash(context.Background(), ag, "/plan")

	_, _, out := replFixture(t, "")
	a.stdout = out
	a.slash(context.Background(), ag, "/permissions")

	// A session refusing everything must be able to say what is refusing it.
	got := out.String()
	if !strings.Contains(got, "deny write(*)") || !strings.Contains(got, "deny bash(*)") {
		t.Fatalf("/permissions did not show the plan-mode rules:\n%s", got)
	}
}

func TestPlanModeTellsTheModelToPlan(t *testing.T) {
	a, ag := planFixture(t)

	a.slash(context.Background(), ag, "/plan")

	// Refusing the tools without saying why produces a model that keeps trying
	// them and reports failures, instead of one that explores and proposes.
	if !strings.Contains(strings.ToLower(ag.ExtraSystem), "plan") {
		t.Fatalf("system prompt does not mention planning: %q", ag.ExtraSystem)
	}

	a.slash(context.Background(), ag, "/plan off")
	if strings.Contains(strings.ToLower(ag.ExtraSystem), "plan") {
		t.Fatalf("leaving plan mode left the instruction behind: %q", ag.ExtraSystem)
	}
}

func TestEnteringPlanModeTwiceIsNotTwoSetsOfRules(t *testing.T) {
	a, ag := planFixture(t)

	a.slash(context.Background(), ag, "/plan")
	first := len(ag.Rules)
	a.slash(context.Background(), ag, "/plan")

	if got := len(ag.Rules); got != first {
		t.Fatalf("rules = %d, want %d", got, first)
	}
}

// Plan mode is state, not a guess from rule text: a session rule someone
// wrote that happens to read like plan mode's is theirs. /new drops it like
// any session rule and announces no plan mode, and /plan still enters plan
// mode properly, instruction included.
func TestARuleThatReadsLikePlanModeIsNotPlanMode(t *testing.T) {
	a, ag := planFixture(t)
	out := &strings.Builder{}
	a.stdout = out
	a.slash(context.Background(), ag, "/permissions deny bash(*) session")
	if a.inPlanMode() {
		t.Fatal("a user's own session rule reads as plan mode")
	}
	a.slash(context.Background(), ag, "/new")
	if len(ag.Rules) != 0 || strings.Contains(out.String(), "plan mode is still on") {
		t.Fatalf("after /new: rules %d, output:\n%s", len(ag.Rules), out.String())
	}

	a.slash(context.Background(), ag, "/permissions deny write(*) session")
	a.slash(context.Background(), ag, "/plan")
	if ag.ExtraSystem != planInstruction {
		t.Fatal("/plan with a look-alike rule in place did not enter plan mode")
	}
	if verdict, _ := ag.Judge(tools.Request{Tool: "bash", Command: "go test ./..."}); verdict != engine.VerdictDeny {
		t.Fatal("plan mode left bash allowed")
	}
	a.slash(context.Background(), ag, "/plan off")
	// Leaving takes plan mode's refusals and leaves the user's own.
	if verdict, _ := ag.Judge(tools.Request{Tool: "write_file", Path: "/p/a.go", Display: "a.go"}); verdict != engine.VerdictDeny {
		t.Fatal("/plan off removed the user's own deny write(*)")
	}
	if verdict, _ := ag.Judge(tools.Request{Tool: "bash", Command: "go test ./..."}); verdict != engine.VerdictAllow {
		t.Fatal("/plan off left bash refused")
	}
}

// Plan mode's refusals are listed under their own scope and go only with plan
// mode: forgetting one would leave plan mode half on.
func TestPlanModesRulesGoOnlyWithPlanMode(t *testing.T) {
	a, ag := planFixture(t)
	out := &strings.Builder{}
	a.stdout = out
	a.slash(context.Background(), ag, "/plan")
	out.Reset()
	a.slash(context.Background(), ag, "/permissions")
	listed := false
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.Contains(line, "deny bash(*)") {
			listed = strings.HasSuffix(strings.TrimSpace(line), " "+scopePlan)
		}
	}
	if !listed {
		t.Fatalf("/permissions does not show plan mode's rules as plan mode's:\n%s", out.String())
	}
	out.Reset()
	a.slash(context.Background(), ag, "/permissions forget 1")
	if !strings.Contains(out.String(), "/plan off") {
		t.Fatalf("forgetting a plan rule said:\n%s", out.String())
	}
	if verdict, _ := ag.Judge(tools.Request{Tool: "write_file", Path: "/p/a.go", Display: "a.go"}); verdict != engine.VerdictDeny {
		t.Fatal("forgetting a plan rule took away one of plan mode's refusals")
	}
}
