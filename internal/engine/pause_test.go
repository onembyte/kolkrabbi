package engine

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/protocol"
)

// The default the owner chose: a limit that will lift stops the turn and keeps
// it -- the input verbatim, the reset time, the reason -- so that nothing is
// lost and nothing is spent until it lifts. A pinned model pauses on itself.
func TestALimitThatWillLiftPausesTheSessionAndKeepsTheTurn(t *testing.T) {
	b := newTestBus(t)
	srv := enginetest.New(enginetest.Step{StatusCode: http.StatusTooManyRequests, RetryAfter: "1800", ErrorBody: `{"error":{"message":"rate limited"}}`})
	defer srv.Close()
	sess := enginetest.NewFakeSession("s_test", "vendor/pinned")
	a := New(Options{
		Client: provider.NewCompatibleClient(srv.URL), Bus: b, Mode: ModeCode, Model: "vendor/pinned", PinnedModel: true,
		Permission: PermissionFullAuto, Out: io.Discard, Sess: sess,
	})
	before := time.Now()
	err := a.RunTurn(context.Background(), "please do the thing")
	var paused *PausedError
	if !errors.As(err, &paused) {
		t.Fatalf("RunTurn error = %v, want a pause", err)
	}
	p := sess.Paused()
	if p == nil || p.PendingTurn != "please do the thing" || p.Kind != string(provider.LimitEndpointCapacity) {
		t.Fatalf("paused = %+v, want the pending input and the kind", p)
	}
	if p.ResetAt.Before(before.Add(29*time.Minute)) || p.ResetAt.After(before.Add(31*time.Minute)) {
		t.Fatalf("ResetAt = %s, want about 30 minutes out (the Retry-After)", p.ResetAt)
	}
	for _, m := range sess.GetMessages() {
		if m.Role == "user" && m.Content == "please do the thing" {
			t.Fatal("the pending turn was left in the transcript as if it had been answered")
		}
	}
	pauses, finishedPaused := 0, 0
	for _, env := range bReplay(t, b) {
		switch env.Type {
		case protocol.EventProviderLimit:
			var d protocol.ProviderLimitData
			_ = json.Unmarshal(env.Data, &d)
			if d.Action == "pause" {
				pauses++
			}
		case protocol.EventTurnFinished:
			var d protocol.TurnFinishedData
			_ = json.Unmarshal(env.Data, &d)
			if d.Reason == "paused" {
				finishedPaused++
			}
		}
	}
	if pauses != 1 || finishedPaused != 1 {
		t.Fatalf("pause events = %d, turn.finished{paused} = %d, want one each", pauses, finishedPaused)
	}

	// Paused means paused: a new prompt spends nothing until the reset.
	requests := len(srv.Requests)
	if err := a.RunTurn(context.Background(), "and this too"); !errors.As(err, &paused) {
		t.Fatalf("a paused session ran a turn: %v", err)
	}
	if len(srv.Requests) != requests {
		t.Fatal("a paused session sent a request")
	}
}

// A limit that waiting cannot lift -- the model refusing this request -- is a
// stop, not a pause, and is published as one.
func TestARefusalThatWaitingCannotLiftIsAStopNotAPause(t *testing.T) {
	b := newTestBus(t)
	srv := enginetest.New(enginetest.Step{StatusCode: http.StatusBadRequest, ErrorBody: `{"error":{"message":"This model's maximum context length is 8192 tokens"}}`})
	defer srv.Close()
	sess := enginetest.NewFakeSession("s_test", "vendor/small")
	a := New(Options{Client: provider.NewCompatibleClient(srv.URL), Bus: b, Mode: ModeCode, Model: "vendor/small", Permission: PermissionFullAuto, Out: io.Discard, Sess: sess})
	if err := a.RunTurn(context.Background(), "hello"); err == nil {
		t.Fatal("a refusal reported success")
	}
	if sess.Paused() != nil {
		t.Fatal("a refusal paused the session")
	}
	stops := 0
	for _, env := range bReplay(t, b) {
		if env.Type == protocol.EventProviderLimit {
			var d protocol.ProviderLimitData
			_ = json.Unmarshal(env.Data, &d)
			if d.Action == "stop" && d.Kind == "model_refusal" {
				stops++
			}
		}
	}
	if stops != 1 {
		t.Fatalf("stop events = %d, want exactly one", stops)
	}
}
