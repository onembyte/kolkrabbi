package engine

import (
	"strings"
	"testing"
)

func TestAPlainStringListStillWorks(t *testing.T) {
	// The planner is a model. Whatever richer shape we ask for, the flat array
	// it produces today has to keep working, or a weaker model breaks the run.
	tasks := parseTasks(`["read the config", "write the test", "run it"]`, 6)

	if len(tasks) != 3 || tasks[0].Title != "read the config" {
		t.Fatalf("parsed %+v", tasks)
	}
	// Without stated dependencies, a task depends on everything before it —
	// exactly the assumption the sequential run already makes.
	if got := tasks[2].Needs; len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Fatalf("needs = %v, want every earlier task", got)
	}
	if tasks[0].Kind != KindUnknown {
		t.Fatalf("kind = %q, want it left unknown rather than guessed", tasks[0].Kind)
	}
}

func TestAStructuredPlanCarriesKindAndDependencies(t *testing.T) {
	tasks := parseTasks(`[
	  {"title": "find the callers", "kind": "research"},
	  {"title": "rename them", "kind": "edit", "needs": [1]},
	  {"title": "update the docs", "kind": "explain"}
	]`, 6)

	if len(tasks) != 3 {
		t.Fatalf("parsed %d tasks", len(tasks))
	}
	if tasks[1].Kind != KindEdit || tasks[1].Title != "rename them" {
		t.Fatalf("second task = %+v", tasks[1])
	}
	// The planner counts from 1, because that is what it was shown in the plan.
	if got := tasks[1].Needs; len(got) != 1 || got[0] != 0 {
		t.Fatalf("needs = %v, want the first task by index", got)
	}
	// A task that states no dependency has none. That is the whole point of
	// asking: "third" and "needs the second" stop being the same claim.
	if got := tasks[2].Needs; len(got) != 0 {
		t.Fatalf("needs = %v, want none", got)
	}
}

func TestNonsenseDependenciesAreDropped(t *testing.T) {
	// A dependency on a later task is a cycle, and on a missing one is a
	// briefing that would silently omit what the task said it needed.
	tasks := parseTasks(`[
	  {"title": "a", "needs": [2]},
	  {"title": "b", "needs": [0, 99, 1, 1]}
	]`, 6)

	if got := tasks[0].Needs; len(got) != 0 {
		t.Fatalf("forward dependency kept: %v", got)
	}
	// 0 is not a task number, 99 is not a task, and the duplicate is one
	// dependency however many times it was written.
	if got := tasks[1].Needs; len(got) != 1 || got[0] != 0 {
		t.Fatalf("needs = %v, want only the valid one", got)
	}
}

func TestAnUnknownKindIsNotGuessed(t *testing.T) {
	tasks := parseTasks(`[{"title": "a", "kind": "refactoring"}]`, 6)
	if tasks[0].Kind != KindUnknown {
		t.Fatalf("kind = %q, want unknown", tasks[0].Kind)
	}
}

func TestThePlanIsStillCappedAndCleaned(t *testing.T) {
	tasks := parseTasks(`[{"title": "a"}, {"title": "   "}, {"title": "b"}, {"title": "c"}]`, 2)
	if len(tasks) != 2 {
		t.Fatalf("parsed %+v, want the cap applied", tasks)
	}
	for _, task := range tasks {
		if strings.TrimSpace(task.Title) == "" {
			t.Fatalf("an empty task survived: %+v", tasks)
		}
	}
}

func TestGarbageIsNoPlan(t *testing.T) {
	for _, reply := range []string{"", "sure, here you go", "{}", "[", `["a"`} {
		if tasks := parseTasks(reply, 6); len(tasks) != 0 {
			t.Fatalf("%q parsed as %+v", reply, tasks)
		}
	}
}

func TestATaskSeesOnlyWhatItAskedFor(t *testing.T) {
	tasks := []Task{
		{Title: "find the callers"},
		{Title: "read the docs"},
		{Title: "rename them", Needs: []int{0}},
	}
	results := []string{"three callers in engine/", "the docs say nothing", ""}

	briefing := dependencyBriefing(tasks, results, 2)

	if !strings.Contains(briefing, "three callers") {
		t.Fatalf("briefing = %q, want the result it depends on", briefing)
	}
	// Handing a task everything is how one subagent's tangent becomes every
	// later subagent's context.
	if strings.Contains(briefing, "the docs say nothing") {
		t.Fatalf("briefing = %q, want only the declared dependency", briefing)
	}
}

func TestATaskWithNoDependenciesGetsNoResults(t *testing.T) {
	tasks := []Task{{Title: "a"}, {Title: "b"}}
	if briefing := dependencyBriefing(tasks, []string{"result of a", ""}, 1); briefing != "" {
		t.Fatalf("briefing = %q, want nothing", briefing)
	}
}
