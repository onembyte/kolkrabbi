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
