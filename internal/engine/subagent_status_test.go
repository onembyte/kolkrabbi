package engine

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
)

func TestSubagentStatusTransitionsAreObservedAndMonotonic(t *testing.T) {
	status := SubagentStatus{
		ID: "k_01ARYZ6S41TSV4RRFFQ69G5FAV", Index: 1, Total: 2,
		Model: "gpt-5.6-luna", Effort: EffortLow, Summary: "inspect the runtime",
	}
	var err error
	status, err = advanceSubagentStatus(status, SubagentQueued, SubagentPhaseSchedule, "  queued\nfor a worker  ")
	if err != nil {
		t.Fatalf("queue transition: %v", err)
	}
	if status.Sequence != 1 || status.Step != "queued for a worker" {
		t.Fatalf("queued status = %+v, want sequence 1 and folded step", status)
	}
	status, err = advanceSubagentStatus(status, SubagentWaiting, SubagentPhaseSchedule, "waiting for task 1")
	if err != nil {
		t.Fatalf("wait transition: %v", err)
	}
	status, err = advanceSubagentStatus(status, SubagentWorking, SubagentPhaseProvider, "asking the model")
	if err != nil {
		t.Fatalf("work transition: %v", err)
	}
	status, err = advanceSubagentStatus(status, SubagentWorking, SubagentPhaseTool, "running tests")
	if err != nil {
		t.Fatalf("step transition: %v", err)
	}
	status, err = advanceSubagentStatus(status, SubagentDone, SubagentPhaseComplete, "completed")
	if err != nil {
		t.Fatalf("done transition: %v", err)
	}
	if status.Sequence != 5 || status.State != SubagentDone || status.Phase != SubagentPhaseComplete {
		t.Fatalf("terminal status = %+v, want ordered sequence 5", status)
	}
}

func TestSubagentStatusRejectsBackwardAndPostTerminalTransitions(t *testing.T) {
	working := SubagentStatus{State: SubagentWorking, Phase: SubagentPhaseProvider, Sequence: 7, Step: "working"}
	if got, err := advanceSubagentStatus(working, SubagentWaiting, SubagentPhaseSchedule, "wait again"); err == nil || got != working {
		t.Fatalf("working -> waiting = %+v, %v; want unchanged rejection", got, err)
	}
	done := SubagentStatus{State: SubagentDone, Phase: SubagentPhaseComplete, Sequence: 8, Step: "done"}
	if got, err := advanceSubagentStatus(done, SubagentWorking, SubagentPhaseTool, "restart"); err == nil || got != done {
		t.Fatalf("done -> working = %+v, %v; want unchanged rejection", got, err)
	}
}

func TestSubagentStatusRejectsUnknownVocabularyAndEmptySteps(t *testing.T) {
	for name, test := range map[string]struct {
		state SubagentState
		phase SubagentPhase
		step  string
	}{
		"state": {state: SubagentState("mystery"), phase: SubagentPhaseSchedule, step: "queued"},
		"phase": {state: SubagentQueued, phase: SubagentPhase("mystery"), step: "queued"},
		"step":  {state: SubagentQueued, phase: SubagentPhaseSchedule, step: " \n\t "},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := advanceSubagentStatus(SubagentStatus{}, test.state, test.phase, test.step); err == nil {
				t.Fatalf("accepted state=%q phase=%q step=%q", test.state, test.phase, test.step)
			}
		})
	}
}

func TestSubagentStepIsBoundedBeforeItReachesASurface(t *testing.T) {
	unsafe := "\x1b[31mworking\x1b[0m\n" + strings.Repeat("x", maxSubagentStepRunes+40)
	status, err := advanceSubagentStatus(SubagentStatus{}, SubagentQueued, SubagentPhaseSchedule, unsafe)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(status.Step, "\x1b") || strings.ContainsAny(status.Step, "\r\n\t") {
		t.Fatalf("step retained terminal controls: %q", status.Step)
	}
	if len([]rune(status.Step)) > maxSubagentStepRunes {
		t.Fatalf("step has %d runes, want at most %d", len([]rune(status.Step)), maxSubagentStepRunes)
	}
}

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
		started.Effort != EffortMax || started.Summary != tasks[0].Title || started.State != SubagentWorking ||
		started.Phase != SubagentPhaseSchedule || started.Step != "started" || started.Sequence != 1 {
		t.Fatalf("started status = %+v", started)
	}
	if finished.Index != 1 || finished.Total != 1 || finished.Model != "gpt-5.6-luna" ||
		finished.Effort != EffortMax || finished.Summary != tasks[0].Title || finished.State != SubagentFailed ||
		finished.Phase != SubagentPhaseComplete || finished.Step != "failed" || finished.Sequence != 2 {
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
	if len(seen) != 4 {
		t.Fatalf("observer received %d updates, want queued, started, provider and done: %+v", len(seen), seen)
	}
	for _, status := range seen {
		if status.Model != "gpt-5.6-luna" || status.Effort != EffortLow {
			t.Fatalf("runtime status = %+v, want task model and computed low effort", status)
		}
	}
	if seen[0].State != SubagentQueued || seen[1].State != SubagentWorking ||
		seen[2].Phase != SubagentPhaseProvider || seen[3].State != SubagentDone {
		t.Fatalf("statuses = %+v; want queued, started, provider, done", seen)
	}
}
