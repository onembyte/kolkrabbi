package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
)

func rosterAgent(model string) *Agent {
	return &Agent{Options: Options{
		Model:         model,
		RungAvailable: func(string, string) bool { return true },
	}}
}

// The point of the whole feature: mechanical work runs on the cheapest model
// the user allows, in its own process, without them having to configure it.
func TestATrivialTaskRunsOnTheCheapestRungTheUserAllows(t *testing.T) {
	agent := rosterAgent("claude-sonnet")
	tasks := []Task{{Title: "commit and push", Kind: KindBoilerplate, Level: LevelTrivial}}
	agent.assignModels(tasks)

	if tasks[0].Model != "claude-haiku" {
		t.Errorf("a trivial task ran on %q, want the cheapest rung", tasks[0].Model)
	}
}

// And the guarantee that pays for it: nothing ever climbs above the user's
// choice, whatever the planner said.
func TestAHardTaskNeverRunsAboveTheModelTheUserChose(t *testing.T) {
	agent := rosterAgent("claude-sonnet")
	tasks := []Task{
		{Title: "design the roster", Kind: KindDesign, Level: LevelHard},
		{Title: "implement it", Kind: KindEdit, Level: LevelRoutine},
	}
	agent.assignModels(tasks)

	for _, task := range tasks {
		if ClampToCeiling(task.Model, "claude-sonnet") != task.Model {
			t.Errorf("%q ran above the ceiling on %q", task.Title, task.Model)
		}
	}
}

// The "X named something off the menu" case. There is no error path, because
// there is no menu of names: an unreadable level is unstated, and unstated
// binds to the ceiling rather than to the cheapest thing available.
func TestAnInventedLevelRunsOnTheModelTheUserChose(t *testing.T) {
	agent := rosterAgent("claude-sonnet")
	tasks := []Task{{Title: "x", Kind: KindEdit, Level: Level("claude-opus")}}
	agent.assignModels(tasks)

	if tasks[0].Model != "claude-sonnet" {
		t.Errorf("an unreadable level resolved to %q, want the user's model", tasks[0].Model)
	}
}

// A slot the user configured is their own decision about their own money, and
// it beats what the planner judged.
func TestAConfiguredSlotStillBeatsALevel(t *testing.T) {
	agent := rosterAgent("claude-opus")
	agent.Slots = map[string]string{SlotFast: "claude-sonnet"}
	tasks := []Task{{Title: "commit", Kind: KindBoilerplate, Level: LevelTrivial}}
	agent.assignModels(tasks)

	if tasks[0].Model != "claude-sonnet" {
		t.Errorf("a trivial task ran on %q, want the slot the user configured", tasks[0].Model)
	}
}

// But not above the ceiling. The slot was configured once; the model was
// selected just now, and the nearer choice is the one that meant it.
func TestTheCeilingStillBeatsAConfiguredSlot(t *testing.T) {
	agent := rosterAgent("claude-sonnet")
	agent.Slots = map[string]string{SlotFast: "claude-opus"}
	tasks := []Task{{Title: "commit", Kind: KindBoilerplate, Level: LevelTrivial}}
	agent.assignModels(tasks)

	if tasks[0].Model != "claude-sonnet" {
		t.Errorf("a slot pointing above the ceiling resolved to %q", tasks[0].Model)
	}
}

// A gateway menu with one model must send the planner, children and synthesis
// to that model, even when slots name models on other vendors. This runs the
// requests rather than comparing two routing helpers that can agree by accident.
func TestAGatewaySessionRoutesExactlyAsItDidBefore(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{Text: `[{"title":"mechanical task","kind":"boilerplate","level":"trivial"},{"title":"research task","kind":"research","level":"routine"}]`},
		enginetest.Step{Text: "mechanical done"},
		enginetest.Step{Text: "research done"},
		enginetest.Step{Text: "final answer"},
	)
	defer srv.Close()
	agent, out, sess, _ := newTestAgentInternal(t, srv, ModeAgent)
	agent.SetSessionModel("gateway/paid-ceiling")
	sess.SetModelName("gateway/paid-ceiling")
	agent.AgentRoster = func(ceiling, _ string) Roster {
		return Roster{Rungs: []Rung{{Model: ceiling}}}
	}
	agent.Slots = map[string]string{SlotFast: "another-vendor/cheap", SlotExplore: "another-vendor/research"}
	agent.MaxConcurrentTasks = 1
	if err := agent.runOrchestrated(context.Background(), "do both gateway tasks"); err != nil {
		t.Fatal(err)
	}
	if len(srv.Models) != 4 {
		t.Fatalf("gateway made %d requests, want planner, two children and synthesis: %v", len(srv.Models), srv.Models)
	}
	for i, model := range srv.Models {
		if model != "gateway/paid-ceiling" {
			t.Errorf("gateway request %d used %q, want the selected model", i, model)
		}
	}
	requestHas := func(index int, want string) bool {
		for _, message := range srv.Requests[index] {
			if strings.Contains(message.Content, want) {
				return true
			}
		}
		return false
	}
	for _, check := range []struct {
		request int
		content string
	}{
		{0, "You are a planning module"},
		{1, "Your task: mechanical task"},
		{2, "Your task: research task"},
		{3, "You are the orchestrator's synthesis step"},
		{3, "Result: mechanical done"},
		{3, "Result: research done"},
	} {
		if !requestHas(check.request, check.content) {
			t.Errorf("gateway request %d did not contain %q: %+v", check.request, check.content, srv.Requests[check.request])
		}
	}
	if messages := sess.GetMessages(); len(messages) != 3 || messages[2].Content != "final answer" {
		t.Errorf("main conversation did not retain the synthesized answer: %+v", messages)
	}
	if !strings.Contains(out.String(), "another-vendor/cheap is outside") ||
		!strings.Contains(out.String(), "another-vendor/research is outside") {
		t.Fatalf("gateway did not announce the rejected out-of-menu slots:\n%s", out.String())
	}
}

// With nothing cheaper signed in, a trivial task runs on the user's model
// rather than failing or being skipped.
func TestATrivialTaskFallsBackToTheCeilingWhenNothingCheaperIsAvailable(t *testing.T) {
	agent := &Agent{Options: Options{
		Model:         "claude-sonnet",
		RungAvailable: func(string, string) bool { return false },
	}}
	tasks := []Task{{Title: "commit", Kind: KindBoilerplate, Level: LevelTrivial}}
	agent.assignModels(tasks)

	if tasks[0].Model != "claude-sonnet" {
		t.Errorf("with nothing cheaper available the task ran on %q", tasks[0].Model)
	}
}

// The roster is resolved once for the whole plan, not once per task: the
// availability answer reads the connector manifest, and a plan of eight tasks
// must not read it eight times — nor risk two tasks disagreeing about what was
// signed in halfway through.
func TestTheRosterIsResolvedOncePerRun(t *testing.T) {
	asked := 0
	agent := &Agent{Options: Options{
		Model:         "claude-sonnet",
		RungAvailable: func(string, string) bool { asked++; return true },
	}}
	tasks := []Task{
		{Title: "a", Level: LevelTrivial}, {Title: "b", Level: LevelTrivial},
		{Title: "c", Level: LevelTrivial}, {Title: "d", Level: LevelTrivial},
	}
	agent.assignModels(tasks)

	// One cheaper rung below sonnet, asked about once for the whole plan.
	if asked != 1 {
		t.Errorf("availability was asked %d times for a four-task plan, want once", asked)
	}
}

// The Fable case, end to end through the roster: a Max session on the top
// rung climbs down to Haiku for mechanical work when the vendor is signed in,
// and stays put for everything else. With nothing signed in, every task runs
// on Fable — and the ceiling never lets anything route above it, because from
// the top there is nowhere to go.
func TestAFableSessionRoutesTrivialWorkToHaikuOnThePlan(t *testing.T) {
	agent := rosterAgent("claude-fable")
	roster := agent.roster(agent.RungAvailable)
	var lane []string
	for _, rung := range roster.Rungs {
		lane = append(lane, rung.Model)
	}
	if got := strings.Join(lane, " → "); got != "claude-fable → claude-opus → claude-sonnet → claude-haiku" {
		t.Fatalf("Fable roster = %q", got)
	}

	tasks := []Task{
		{Title: "commit", Kind: KindBoilerplate, Level: LevelTrivial},
		{Title: "implement", Kind: KindEdit, Level: LevelRoutine},
		{Title: "design", Kind: KindDesign, Level: LevelHard},
	}
	agent.assignModels(tasks)
	if tasks[0].Model != "claude-haiku" || tasks[1].Model != "claude-opus" || tasks[2].Model != "claude-fable" {
		t.Fatalf("models = %q / %q / %q, want haiku / opus / fable", tasks[0].Model, tasks[1].Model, tasks[2].Model)
	}

	alone := &Agent{Options: Options{Model: "claude-fable"}}
	alone.assignModels(tasks)
	for _, task := range tasks {
		if task.Model != "claude-fable" {
			t.Fatalf("%q ran on %q with nothing signed in, want claude-fable", task.Title, task.Model)
		}
	}
	if above := ModelsAboveCeiling("claude-fable"); len(above) != 0 {
		t.Fatalf("something sits above the top rung: %v", above)
	}
	if below := ModelsBelowCeiling("claude-fable"); strings.Join(below, ",") != "claude-opus,claude-sonnet,claude-haiku" {
		t.Fatalf("below fable = %v", below)
	}
	if below := ModelsBelowCeiling("claude-haiku"); len(below) != 0 {
		t.Fatalf("something sits below the bottom rung: %v", below)
	}
}
