package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
)

func TestARunAddsUpWhatItSpent(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{Text: `[{"title":"a"},{"title":"b"}]`, Cost: 0.01},
		enginetest.Step{Text: "did a", Cost: 0.02},
		enginetest.Step{Text: "did b", Cost: 0.03},
		enginetest.Step{Text: "answer", Cost: 0.04},
	)
	defer srv.Close()

	agent, out, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	if err := agent.runOrchestrated(context.Background(), "two things"); err != nil {
		t.Fatalf("run returned %v", err)
	}

	// Making the number visible is most of the value: an orchestrated run is
	// the one place a single typed line can quietly become several dollars.
	if !strings.Contains(out.String(), "0.10") {
		t.Fatalf("the run never showed its total; output:\n%s", out.String())
	}
}

func TestACeilingStopsTheRunAndKeepsTheWork(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{Text: `[{"title":"a"},{"title":"b"},{"title":"c"}]`, Cost: 0.10},
		enginetest.Step{Text: "did a", Cost: 0.40},
		enginetest.Step{Text: "answer"},
	)
	defer srv.Close()

	agent, out, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	agent.Effort = EffortHigh
	agent.MaxRunCostUSD = 0.25

	if err := agent.runOrchestrated(context.Background(), "three things"); err != nil {
		t.Fatalf("a run over budget reported an error: %v", err)
	}

	// A stop, not a refusal. The task that finished cost money and is still
	// worth delivering.
	if got := len(srv.Requests); got != 3 {
		t.Fatalf("%d requests, want plan + one task + synthesis", got)
	}
	got := strings.ToLower(out.String())
	if !strings.Contains(got, "budget") {
		t.Fatalf("the user was not told why it stopped:\n%s", out.String())
	}
}

func TestNoCeilingMeansNoCeiling(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{Text: `[{"title":"a"},{"title":"b"}]`, Cost: 5},
		enginetest.Step{Text: "did a", Cost: 5},
		enginetest.Step{Text: "did b", Cost: 5},
		enginetest.Step{Text: "answer", Cost: 5},
	)
	defer srv.Close()

	agent, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	// A default ceiling nobody chose would be a surprise the first time it
	// truncated real work.
	if err := agent.runOrchestrated(context.Background(), "two things"); err != nil {
		t.Fatalf("run returned %v", err)
	}
	if got := len(srv.Requests); got != 4 {
		t.Fatalf("%d requests, want the whole run", got)
	}
}

func TestSynthesisIsToldTheRunWasCutShort(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{Text: `[{"title":"gather"},{"title":"decide"}]`, Cost: 0.5},
		enginetest.Step{Text: "gathered", Cost: 0.5},
		enginetest.Step{Text: "answer"},
	)
	defer srv.Close()

	agent, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	agent.MaxRunCostUSD = 0.75
	agent.runOrchestrated(context.Background(), "gather then decide")

	// An answer that reads as complete when a third of the plan never ran is
	// the failure mode the whole outcome machinery exists to prevent.
	synthesis := lastRequestText(t, srv)
	if !strings.Contains(synthesis, "decide") || !strings.Contains(strings.ToLower(synthesis), "budget") {
		t.Fatalf("synthesis was not told:\n%s", synthesis)
	}
}

func TestTheCeilingIsCheckedBeforeSpendingNotAfter(t *testing.T) {
	tracker := &spend{limit: 1.0}
	tracker.add(0.99)
	if tracker.exhausted() {
		t.Fatal("stopped while there was still budget")
	}
	tracker.add(0.02)
	if !tracker.exhausted() {
		t.Fatalf("total %.2f did not exhaust a 1.00 ceiling", tracker.total())
	}

	// No limit is not a limit of zero.
	none := &spend{}
	none.add(1000)
	if none.exhausted() {
		t.Fatal("an unlimited run stopped itself")
	}
}
