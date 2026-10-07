package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
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

// A reply with no readable plan still runs the request directly, so no work
// is lost, but it is not called a single-step task: a plan cut off at the
// planner's output limit would otherwise vanish without a word. A genuine
// one-task plan keeps its own line.
func TestAnUnreadablePlanIsNotCalledASingleStep(t *testing.T) {
	var planned []string
	for i := range 40 {
		planned = append(planned, fmt.Sprintf(`{"title":"task %d","kind":"research","level":"routine","needs":[%d]}`, i+1, i))
	}
	full := "[" + strings.Join(planned, ",") + "]"
	for _, c := range []struct {
		name, reply, want string
	}{
		{"cut off", full[:len(full)*2/3], "it may have been cut off"},
		{"prose", "Sure, I would start by reading the config.", "the planner answered without a plan"},
		{"empty", "", "the planner's reply was empty"},
		{"empty plan", "[]", "the planner's plan had no tasks"},
		{"wrong types", `[{"title":"a","needs":["1"]},{"title":"b"}]`, "the planner's plan could not be read; running the request directly"},
		{"one of two usable", `[{"task":"untitled"},{"title":"b"}]`, "only 1 of the planner's 2 tasks could be read"},
		{"no title", `[{}]`, "the planner's one task could not be read; running the request directly"},
		{"null task", `[null]`, "the planner's one task could not be read; running the request directly"},
		{"none usable", `[{},{"task":"untitled"}]`, "none of the planner's 2 tasks could be read; running the request directly"},
		{"one task", `[{"title":"fix the typo"}]`, "single-step task"},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := enginetest.New(enginetest.Step{Text: c.reply}, enginetest.Step{Text: "did it directly"})
			defer srv.Close()
			a, out, _, _ := newTestAgentInternal(t, srv, ModeAgent)
			work := &detailWork{}
			a.Work = work
			if err := a.RunTurn(context.Background(), "a request"); err != nil {
				t.Fatal(err)
			}
			// The screen's activity line says the same as the transcript.
			published := "running the request directly: no readable plan"
			if c.name == "one task" {
				published = "running a single task directly"
			}
			if !slices.Contains(work.steps, published) {
				t.Errorf("published work = %q; want %q", work.steps, published)
			}
			text := out.String()
			if !strings.Contains(text, c.want) || !strings.Contains(text, "did it directly") {
				t.Fatalf("output lacks %q or the direct answer:\n%s", c.want, text)
			}
			if c.name != "one task" && strings.Contains(text, "single-step task") {
				t.Fatalf("a reply with no readable plan was called a single step:\n%s", text)
			}
		})
	}
}

// Adopted from the V43.5 verification (V2): a run that fell back to a direct
// run and then paused is resumed as what it is, not re-announced as a
// single-step task the planner never chose.
func TestAResumedDirectRunIsNotCalledASingleStep(t *testing.T) {
	limit := enginetest.Step{StatusCode: http.StatusTooManyRequests, RetryAfter: "1800",
		ErrorBody: `{"error":{"message":"You have reached your usage limit"}}`}
	srv := enginetest.New(
		enginetest.Step{Text: `[{"title":"a","needs":[]},{"title":"b","needs":[1]},{"title":"c","ne`},
		limit,
		enginetest.Step{Text: "done after resume"},
	)
	defer srv.Close()
	a, out, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	var paused *PausedError
	if err := a.RunTurn(context.Background(), "big request"); !errors.As(err, &paused) {
		t.Fatalf("want a pause: %v\n%s", err, out.String())
	}
	out.Reset()
	pending, ok := a.Resume()
	if !ok {
		t.Fatal("no claim to resume")
	}
	if err := a.RunTurn(context.Background(), pending); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if text := out.String(); strings.Contains(text, "single-step task") || !strings.Contains(text, "resuming the request directly") {
		t.Fatalf("the resumed direct run was announced as:\n%s", text)
	}
}
