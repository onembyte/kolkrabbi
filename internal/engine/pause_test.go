package engine

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
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
	if run := sess.RunState(); run == nil || run.Phase != "direct" || run.Input != p.PendingTurn {
		t.Fatalf("pending request has no unfinished journal: %+v", run)
	}
	if count, reasons := sess.RecoverySaves(); count != 1 || len(reasons) != 1 || reasons[0] != "pause" {
		t.Fatalf("recovery saves = %d %v, want one pause boundary", count, reasons)
	}
	msgs := sess.GetMessages()
	if last := msgs[len(msgs)-1]; last.Role != "user" || last.Content != p.PendingTurn {
		t.Fatalf("paused transcript lost its unanswered prompt: %+v", msgs)
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
	if count, reasons := sess.RecoverySaves(); count != 1 || len(reasons) != 1 || reasons[0] != "limit" {
		t.Fatalf("recovery saves = %d %v, want one non-pausable limit boundary", count, reasons)
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

// recoveringDiskSession is a real fake session whose saves fail on demand.
type recoveringDiskSession struct {
	*enginetest.FakeSession
	mu   sync.Mutex
	fail bool
}

func (s *recoveringDiskSession) setFail(v bool) { s.mu.Lock(); s.fail = v; s.mu.Unlock() }

func (s *recoveringDiskSession) err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return errors.New("no space left on device")
	}
	return nil
}

func (s *recoveringDiskSession) Save() error {
	if err := s.err(); err != nil {
		return err
	}
	return s.FakeSession.Save()
}

func (s *recoveringDiskSession) SaveInterim() error {
	if err := s.err(); err != nil {
		return err
	}
	return s.FakeSession.SaveInterim()
}

func (s *recoveringDiskSession) SaveRecovery(reason string) error {
	if err := s.err(); err != nil {
		return err
	}
	return s.FakeSession.SaveRecovery(reason)
}

// "Failed persistence is visible" across a whole session: saves fail and are
// reported, recover, then fail again as a usage limit pauses the turn. The
// pause exists only in memory, so the person must be told, not just shown
// "◆ paused".
func TestAPauseThatCouldNotBeSavedSaysSo(t *testing.T) {
	limit := enginetest.Step{StatusCode: http.StatusTooManyRequests, RetryAfter: "1800",
		ErrorBody: `{"error":{"message":"You have reached your usage limit"}}`}
	srv := enginetest.New(enginetest.Step{Text: "first answer"}, enginetest.Step{Text: "second answer"},
		enginetest.Step{Text: "a title"}, limit, limit, limit)
	defer srv.Close()
	a, out, fake, _ := newTestAgentInternal(t, srv, ModeCode)
	sess := &recoveringDiskSession{FakeSession: fake}
	a.Sess = sess

	sess.setFail(true)
	_ = a.RunTurn(context.Background(), "turn one")
	sess.setFail(false)
	_ = a.RunTurn(context.Background(), "turn two")
	sess.setFail(true)
	out.Reset()
	err := a.RunTurn(context.Background(), "turn three")
	var paused *PausedError
	if err == nil || errors.As(err, &paused) || !strings.Contains(err.Error(), "restart recovery is not guaranteed") {
		t.Fatalf("turn three = %v; want a visible recovery failure without a durable-pause claim", err)
	}
	if sess.Paused() != nil {
		t.Fatal("a failed recovery save armed automatic resume")
	}
	if !strings.Contains(out.String(), "could not save the pause recovery point") {
		t.Fatalf("the pause was not saved and nothing said so:\n%s", out.String())
	}
}

// Adopted from the V43.5 verification (V4): machine surfaces follow the same
// rule as the terminal. kolk's own estimate is never published as the
// vendor's reset: the resume event carries no reset_at (absent when unknown,
// per protocol), and the paused turn says when kolk checks next, not "until".
// A time the vendor gave is still published as its reset.
func TestTheBusNeverPresentsAnEstimateAsAReset(t *testing.T) {
	for _, c := range []struct {
		name       string
		retryAfter string
		estimated  bool
	}{
		{"assumed", "", true},
		{"vendor's Retry-After", "1800", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := enginetest.New(enginetest.Step{StatusCode: http.StatusTooManyRequests, RetryAfter: c.retryAfter,
				ErrorBody: `{"error":{"message":"You have reached your usage limit"}}`}, enginetest.Step{Text: "after resume"})
			defer srv.Close()
			a, _, _, _ := newTestAgentInternal(t, srv, ModeCode)
			b := newTestBus(t)
			a.Bus = b
			var paused *PausedError
			if err := a.RunTurn(context.Background(), "hello"); !errors.As(err, &paused) || paused.Pause.Estimated != c.estimated {
				t.Fatalf("pause = %v %+v; want estimated=%v", err, paused, c.estimated)
			}
			pending, ok := a.Resume()
			if !ok {
				t.Fatal("no claim to resume")
			}
			_ = a.RunTurn(context.Background(), pending)
			sub, err := b.Subscribe(0)
			if err != nil {
				t.Fatal(err)
			}
			defer sub.Close()
			resumed, finished := false, false
			for _, env := range sub.Replay() {
				switch env.Type {
				case protocol.EventProviderLimit:
					var d protocol.ProviderLimitData
					_ = json.Unmarshal(env.Data, &d)
					if d.Action == "resume" {
						resumed = true
						if (d.ResetAt != "") == c.estimated {
							t.Errorf("provider.limit{resume}.reset_at = %q; estimated=%v", d.ResetAt, c.estimated)
						}
					}
				case protocol.EventTurnFinished:
					var d protocol.TurnFinishedData
					_ = json.Unmarshal(env.Data, &d)
					if d.Reason == "paused" {
						finished = true
						if strings.Contains(d.RawReason, " until ") == c.estimated {
							t.Errorf("paused raw_reason = %q; estimated=%v", d.RawReason, c.estimated)
						}
					}
				}
			}
			if !resumed || !finished {
				t.Fatalf("missing events: resume=%v paused=%v", resumed, finished)
			}
		})
	}
}
