package engine

import (
	"context"
	"sync"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
)

func TestSubagentObserverReceivesTheWholeLifecycle(t *testing.T) {
	var seen []SubagentStatus
	a := &Agent{Options: Options{
		Mode:   ModeAgent,
		Effort: EffortHigh,
		Subagents: func(status SubagentStatus) {
			seen = append(seen, status)
		},
	}}
	a.lastTurnID = "t_01ARYZ6S41TSV4RRFFQ69G5FAW"
	tasks := []Task{{
		Title: "Review the runtime\nand report the important defects",
		Kind:  KindResearch,
		Level: LevelHard,
		Model: "gpt-5.6-luna",
	}}
	child := "t_01ARYZ6S41TSV4RRFFQ69G5FAX"

	a.publishSubagentStarted(tasks, 0, child, "gpt-5.6-luna", EffortMax)
	a.publishSubagentFinished(child, 0, false, "gpt-5.6-luna", EffortMax)

	if len(seen) != 2 {
		t.Fatalf("observer received %d updates, want start and finish", len(seen))
	}
	started, finished := seen[0], seen[1]
	if started.ID == "" || started.ID != finished.ID {
		t.Fatalf("lifecycle ids = %q then %q, want one stable id", started.ID, finished.ID)
	}
	if started.Index != 1 || started.Total != 1 || started.Model != "gpt-5.6-luna" ||
		started.Effort != EffortMax || started.Summary != tasks[0].Title || started.State != SubagentWorking {
		t.Fatalf("started status = %+v", started)
	}
	if finished.Index != 1 || finished.Total != 1 || finished.Model != "gpt-5.6-luna" ||
		finished.Effort != EffortMax || finished.Summary != tasks[0].Title || finished.State != SubagentFailed {
		t.Fatalf("finished status = %+v", finished)
	}
}

func TestARealTaskReportsItsComputedEffortAndModel(t *testing.T) {
	srv := enginetest.New(enginetest.Step{Text: "done"})
	defer srv.Close()
	agent, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	agent.Effort = EffortHigh
	agent.MaxConcurrentTasks = 1

	var mu sync.Mutex
	var seen []SubagentStatus
	agent.Subagents = func(status SubagentStatus) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, status)
	}
	tasks := []Task{{
		Title: "mechanical check", Kind: KindResearch,
		Level: LevelTrivial, Model: "gpt-5.6-luna",
	}}
	if _, err := agent.runTasks(context.Background(), "check it", tasks); err != nil {
		t.Fatalf("runTasks: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 {
		t.Fatalf("observer received %d updates, want two: %+v", len(seen), seen)
	}
	for _, status := range seen {
		if status.Model != "gpt-5.6-luna" || status.Effort != EffortLow {
			t.Fatalf("runtime status = %+v, want task model and computed low effort", status)
		}
	}
	if seen[0].State != SubagentWorking || seen[1].State != SubagentDone {
		t.Fatalf("states = %q then %q, want working then done", seen[0].State, seen[1].State)
	}
}
