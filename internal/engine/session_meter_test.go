package engine

import (
	"context"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
)

func TestSessionCostStartsAtNothing(t *testing.T) {
	agent, _, _, _ := newTestAgentInternal(t, enginetest.New(), ModeCode)
	if got := agent.SessionCostUSD(); got != 0 {
		t.Fatalf("a fresh session already cost $%.4f", got)
	}
}

func TestSessionCostAddsUpEveryCall(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{Text: "one", Cost: 0.01},
		enginetest.Step{Text: "two", Cost: 0.02},
	)
	defer srv.Close()

	agent, _, _, _ := newTestAgentInternal(t, srv, ModeCode)
	for _, prompt := range []string{"a", "b"} {
		if err := agent.RunTurn(context.Background(), prompt); err != nil {
			t.Fatalf("turn: %v", err)
		}
	}

	// The per-call footer answers "what did that cost". The status line has to
	// answer "what has this session cost", and nothing was tracking it.
	if got := agent.SessionCostUSD(); got < 0.029 || got > 0.031 {
		t.Fatalf("session cost = %.4f, want 0.03", got)
	}
}

func TestOrchestrationCostCountsTowardTheSession(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{Text: `[{"title":"a"},{"title":"b"}]`, Cost: 0.10},
		enginetest.Step{Text: "did a", Cost: 0.20},
		enginetest.Step{Text: "did b", Cost: 0.30},
		enginetest.Step{Text: "answer", Cost: 0.40},
	)
	defer srv.Close()

	agent, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	agent.MaxConcurrentTasks = 1
	if err := agent.runOrchestrated(context.Background(), "two things"); err != nil {
		t.Fatalf("run: %v", err)
	}

	// A run's ceiling is per-run; the session meter is not reset by it.
	if got := agent.SessionCostUSD(); got < 0.99 || got > 1.01 {
		t.Fatalf("session cost = %.4f, want 1.00", got)
	}
}

func TestContextUsageIsReadableFromOutside(t *testing.T) {
	agent, _, _, _ := newTestAgentInternal(t, enginetest.New(), ModeCode)
	agent.ContextWindow = 1000
	agent.lastPromptTokens.Store(250)

	usage := agent.Context()

	if usage.Window != 1000 || usage.Used != 250 {
		t.Fatalf("usage = %+v", usage)
	}
	if label := usage.Label(); label == "" {
		t.Fatal("a measured window produced no label")
	}
}
