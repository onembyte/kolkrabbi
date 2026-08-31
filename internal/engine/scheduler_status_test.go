package engine

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
)

type statusRecorder struct {
	mu       sync.Mutex
	statuses []SubagentStatus
}

func (r *statusRecorder) record(status SubagentStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statuses = append(r.statuses, status)
}

func (r *statusRecorder) task(index int) []SubagentStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	var found []SubagentStatus
	for _, status := range r.statuses {
		if status.Index == index {
			found = append(found, status)
		}
	}
	return found
}

func TestSchedulerQueuesEveryTaskBeforeAnyTaskStarts(t *testing.T) {
	srv := enginetest.New(enginetest.Step{Text: "first"}, enginetest.Step{Text: "second"})
	defer srv.Close()
	a, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	a.MaxConcurrentTasks = 2
	var seen statusRecorder
	a.Subagents = seen.record
	tasks := []Task{
		{Title: "first", Kind: KindResearch, Model: "gpt-5.6-luna"},
		{Title: "second", Kind: KindResearch, Model: "gpt-5.6-luna"},
	}

	if _, err := a.runTasks(context.Background(), "inspect", tasks); err != nil {
		t.Fatal(err)
	}
	seen.mu.Lock()
	defer seen.mu.Unlock()
	if len(seen.statuses) < 2 || seen.statuses[0].Index != 1 || seen.statuses[1].Index != 2 ||
		seen.statuses[0].State != SubagentQueued || seen.statuses[1].State != SubagentQueued {
		t.Fatalf("first scheduler updates = %+v, want every task queued in plan order", seen.statuses)
	}
}

func TestSchedulerExplainsDependencyAndWriterWaiting(t *testing.T) {
	t.Run("dependency", func(t *testing.T) {
		srv := enginetest.New(enginetest.Step{Text: "found"}, enginetest.Step{Text: "used it"})
		defer srv.Close()
		a, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
		a.MaxConcurrentTasks = 2
		var seen statusRecorder
		a.Subagents = seen.record
		tasks := []Task{
			{Title: "find", Kind: KindResearch, Model: "gpt-5.6-luna"},
			{Title: "use", Kind: KindResearch, Needs: []int{0}, Model: "gpt-5.6-luna"},
		}
		if _, err := a.runTasks(context.Background(), "find then use", tasks); err != nil {
			t.Fatal(err)
		}
		assertStatusStep(t, seen.task(2), SubagentWaiting, "waiting for task 1")
	})

	t.Run("shared tree writer", func(t *testing.T) {
		srv := enginetest.New(enginetest.Step{Text: "edited one"}, enginetest.Step{Text: "edited two"})
		defer srv.Close()
		a, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
		a.MaxConcurrentTasks = 2
		var seen statusRecorder
		a.Subagents = seen.record
		tasks := []Task{
			{Title: "edit one", Kind: KindEdit, Model: "gpt-5.6-luna"},
			{Title: "edit two", Kind: KindEdit, Model: "gpt-5.6-luna"},
		}
		if _, err := a.runTasks(context.Background(), "two edits", tasks); err != nil {
			t.Fatal(err)
		}
		assertStatusStep(t, seen.task(2), SubagentWaiting, "waiting for the shared-tree writer")
	})
}

func TestSchedulerMarksNeverStartedTasksBlockedWithoutCountingThem(t *testing.T) {
	a := &Agent{Options: Options{Mode: ModeAgent, Out: io.Discard, MaxRunCostUSD: 1}}
	a.lastTurnID = "t_01ARYZ6S41TSV4RRFFQ69G5FAW"
	a.runSpend = &spend{limit: 1, usd: 1}
	var seen statusRecorder
	a.Subagents = seen.record

	outcomes, err := a.runTasks(context.Background(), "do it", []Task{{
		Title: "over budget", Kind: KindResearch, Model: "gpt-5.6-luna",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if outcomes[0].Status != statusOverBudget {
		t.Fatalf("outcome = %s, want over budget", outcomes[0].Status)
	}
	assertStatusStep(t, seen.task(1), SubagentBlocked, "budget")
	if got := a.RunningSubagents(); got != 0 {
		t.Fatalf("never-started task raised running count to %d", got)
	}
}

func TestActiveTaskReportsCheckpointProviderAndTerminalBoundaries(t *testing.T) {
	srv := enginetest.New(enginetest.Step{Text: "done"})
	defer srv.Close()
	a, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	a.MaxConcurrentTasks = 1
	a.Ckpt = &enginetest.FakeCheckpointer{}
	var seen statusRecorder
	a.Subagents = seen.record

	if _, err := a.runTasks(context.Background(), "edit it", []Task{{
		Title: "edit the file", Kind: KindEdit, Model: "gpt-5.6-luna",
	}}); err != nil {
		t.Fatal(err)
	}
	statuses := seen.task(1)
	assertStatusStep(t, statuses, SubagentWorking, "creating rollback checkpoint")
	assertStatusStep(t, statuses, SubagentWorking, "opening gpt-5.6-luna")
	if statuses[len(statuses)-1].State != SubagentDone || statuses[len(statuses)-1].Step != "completed" {
		t.Fatalf("terminal status = %+v, want completed", statuses[len(statuses)-1])
	}
	for index, status := range statuses {
		if status.Sequence != uint64(index+1) {
			t.Fatalf("status sequence[%d] = %d, want %d: %+v", index, status.Sequence, index+1, statuses)
		}
	}
}

func TestActiveTaskFailureKeepsTheProviderReason(t *testing.T) {
	a := &Agent{Options: Options{
		Mode: ModeAgent, Model: "gpt-5.6-luna", Out: io.Discard,
		SubagentBackend: func(context.Context, string, string, string) (ChatBackend, error) {
			return nil, errors.New("provider would not start")
		},
	}}
	a.lastTurnID = "t_01ARYZ6S41TSV4RRFFQ69G5FAW"
	var seen statusRecorder
	a.Subagents = seen.record

	outcomes, err := a.runTasks(context.Background(), "inspect", []Task{{
		Title: "inspect", Kind: KindResearch, Model: "gpt-5.6-luna",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if outcomes[0].Status != statusFailed {
		t.Fatalf("outcome = %s, want failed", outcomes[0].Status)
	}
	statuses := seen.task(1)
	assertStatusStep(t, statuses, SubagentWorking, "opening gpt-5.6-luna")
	assertStatusStep(t, statuses, SubagentFailed, "provider would not start")
	terminal := 0
	for _, status := range statuses {
		if status.State == SubagentDone || status.State == SubagentFailed || status.State == SubagentBlocked {
			terminal++
		}
	}
	if terminal != 1 || statuses[len(statuses)-1].State != SubagentFailed ||
		!strings.Contains(statuses[len(statuses)-1].Step, "provider would not start") {
		t.Fatalf("terminal statuses = %+v, want one reason-preserving failure", statuses)
	}
}

func assertStatusStep(t *testing.T, statuses []SubagentStatus, state SubagentState, contains string) {
	t.Helper()
	for _, status := range statuses {
		if status.State == state && strings.Contains(status.Step, contains) {
			return
		}
	}
	t.Fatalf("statuses = %+v, want %s step containing %q", statuses, state, contains)
}
