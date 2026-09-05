package engine

import (
	"context"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
)

// The ceiling is checked before a call, and a call's cost is known only after
// it. With N tasks admitted together under the ceiling, N calls spend at once
// and the run can end N times over it. Admission reserves the worst cost seen
// so far for every task still in flight, so in-flight calls together cannot
// cross the ceiling: the run may exceed it by at most one call, as a
// sequential run always could, never by a whole wave.
func TestInFlightCallsCannotCrossTheRunBudgetTogether(t *testing.T) {
	const perCall, ceiling = 0.60, 1.00
	held := enginetest.Step{Text: "done", Cost: perCall, Delay: 60 * time.Millisecond}
	srv := enginetest.New(held, held, held, held)
	defer srv.Close()
	agent, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	agent.MaxConcurrentTasks = 4
	agent.MaxRunCostUSD = ceiling
	agent.runSpend = &spend{limit: ceiling}

	outcomes, err := agent.runTasks(context.Background(), "four things", []Task{
		{Title: "a", Kind: KindResearch}, {Title: "b", Kind: KindResearch},
		{Title: "c", Kind: KindResearch}, {Title: "d", Kind: KindResearch},
	})
	if err != nil {
		t.Fatal(err)
	}
	total := agent.runSpend.total()
	if total > ceiling+perCall {
		t.Fatalf("the run spent $%.2f against a $%.2f ceiling; in-flight calls crossed it together", total, ceiling)
	}
	over := 0
	for _, o := range outcomes {
		if o.Status == statusOverBudget {
			over++
		}
	}
	if over == 0 {
		t.Fatalf("no task was stopped by the budget; outcomes = %+v", outcomes)
	}
}

// Reservation must not become serialization: once the first call has shown
// what a call costs, cheap tasks under a generous ceiling still overlap.
func TestCalibratedCheapTasksStillRunConcurrently(t *testing.T) {
	cheap := enginetest.Step{Text: "done", Cost: 0.001, Delay: 60 * time.Millisecond}
	srv := enginetest.New(cheap, cheap, cheap, cheap)
	defer srv.Close()
	agent, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	agent.MaxConcurrentTasks = 4
	agent.MaxRunCostUSD = 10
	agent.runSpend = &spend{limit: 10}

	if _, err := agent.runTasks(context.Background(), "four cheap things", []Task{
		{Title: "a", Kind: KindResearch}, {Title: "b", Kind: KindResearch},
		{Title: "c", Kind: KindResearch}, {Title: "d", Kind: KindResearch},
	}); err != nil {
		t.Fatal(err)
	}
	if got := srv.MaxInFlight(); got < 2 {
		t.Fatalf("max in flight = %d; the budget reservation serialized a run that had room", got)
	}
}
