package engine

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/protocol"
)

// A turn that ends in an ordinary error -- the provider refused, a tool broke
// -- is as over as one that finished or was cancelled, and a subscriber that
// saw turn.started must see exactly one terminal turn event say so. Cancelled
// turns already do; this pins the third ending. The reason vocabulary of
// turn.finished is open by contract, so the error is a finished turn with
// reason "error" and the scrubbed message as raw_reason.
func TestAnOrdinaryErrorEndsTheTurnWithOneTerminalEvent(t *testing.T) {
	srv := enginetest.New(enginetest.Step{StatusCode: 400, ErrorBody: `{"error":{"message":"model exploded"}}`})
	defer srv.Close()
	b := newTestBus(t)
	a := New(Options{
		Client: provider.NewCompatibleClient(srv.URL), Bus: b, Mode: ModeCode, Model: "mock/model",
		Permission: PermissionFullAuto, Out: io.Discard, Sess: enginetest.NewFakeSession("s_test", "mock/model"),
	})
	if err := a.RunTurn(context.Background(), "do something"); err == nil {
		t.Fatal("a 400 from the provider did not fail the turn")
	}
	finished, cancelled := 0, 0
	for _, envelope := range bReplay(t, b) {
		switch envelope.Type {
		case protocol.EventTurnFinished:
			finished++
			var data protocol.TurnFinishedData
			if err := json.Unmarshal(envelope.Data, &data); err != nil {
				t.Fatal(err)
			}
			if data.Reason != "error" || data.RawReason == "" {
				t.Fatalf("terminal event for an errored turn = %+v, want reason error with a raw_reason", data)
			}
		case protocol.EventTurnCancelled:
			cancelled++
		}
	}
	if finished != 1 || cancelled != 0 {
		t.Fatalf("finished/cancelled = %d/%d, want exactly one finished and no cancelled", finished, cancelled)
	}
}
