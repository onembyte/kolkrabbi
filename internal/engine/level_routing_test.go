package engine

import (
	"strings"
	"testing"
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
		if task.Model != "claude-sonnet" {
			t.Errorf("%q ran on %q, want the model the user selected", task.Title, task.Model)
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

// A gateway session has no ladder, so nothing about its routing may change.
func TestAGatewaySessionRoutesExactlyAsItDidBefore(t *testing.T) {
	agent := &Agent{Options: Options{Model: "openrouter/free"}}
	tasks := []Task{
		{Title: "a", Kind: KindBoilerplate, Level: LevelTrivial},
		{Title: "b", Kind: KindEdit, Level: LevelHard},
	}
	agent.assignModels(tasks)

	for index, task := range tasks {
		if task.Model != agent.modelForKind(tasks[index].Kind) {
			t.Errorf("%q routed to %q, want what kind-based routing gives", task.Title, task.Model)
		}
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
	if tasks[0].Model != "claude-haiku" || tasks[1].Model != "claude-fable" || tasks[2].Model != "claude-fable" {
		t.Fatalf("models = %q / %q / %q, want haiku / fable / fable", tasks[0].Model, tasks[1].Model, tasks[2].Model)
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
