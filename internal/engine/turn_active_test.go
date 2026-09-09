package engine_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
)

// An agent says while a turn is running, and stops saying it the moment the
// turn is over. It is what lets the surface's background vendor discovery
// stand aside for the person who is waiting on tokens (OPTIMIZATION_PLAN.md
// O8.3) without the engine knowing anything about vendors.
func TestAnAgentSaysWhileATurnIsRunning(t *testing.T) {
	srv := enginetest.New(enginetest.Step{Text: "done."})
	defer srv.Close()
	ag, _, _, _ := newTestAgent(t, srv, "chat")

	if ag.TurnActive() {
		t.Fatal("an idle agent reports a turn")
	}

	// Observed from inside the turn: the recorder is called when the model
	// call it is recording has just finished, which is unambiguously during.
	var during atomic.Bool
	ag.Recorder = &flagObservingRecorder{observe: func() { during.Store(ag.TurnActive()) }}

	if err := ag.RunTurn(context.Background(), "hello"); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	if !during.Load() {
		t.Error("the agent did not report a turn while one was running")
	}
	if ag.TurnActive() {
		t.Error("the agent still reports a turn after it ended")
	}
}

type flagObservingRecorder struct{ observe func() }

func (r *flagObservingRecorder) RecordCall(engine.CallRecord) error { r.observe(); return nil }
func (r *flagObservingRecorder) RecordRating(string, string, int) error {
	r.observe()
	return nil
}
