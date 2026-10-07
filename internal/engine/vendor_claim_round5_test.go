package engine

// Found by the round-5 vendor-recovery verifier: the claim release was keyed
// by run ID, so a delivery armed inside a turn could lose its claim.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

// round5GateWriter holds the first matching write, once armed, until gate closes
// (bounded), so a goroutine the write races with can run first. It forces one
// real interleaving; it changes nothing the engine does.
type round5GateWriter struct {
	mu    sync.Mutex
	buf   strings.Builder
	match string
	armed atomic.Bool
	gate  chan struct{}
	once  sync.Once
	held  atomic.Bool
}

func (w *round5GateWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.buf.Write(p)
	w.mu.Unlock()
	if w.armed.Load() && strings.Contains(string(p), w.match) {
		w.once.Do(func() {
			select {
			case <-w.gate:
				w.held.Store(true)
			case <-time.After(5 * time.Second):
			}
		})
	}
	return len(p), nil
}

// R5-P2. Continuity on, vendor main session. A resumed turn (R1, claim X)
// pauses again; ContinueOn refuses (vendor conversation) and its deferred
// armResume arms a monitor INSIDE R1, before R1's claim-release defer. If the
// monitor delivers first (its reset is due), it claims X; R1's release then
// sees claim == X and clears the monitor's claim. /resume then hands out X a
// second time while the monitor's delivery is still pending (REPL: waiting on
// turnMu). Whichever runs second finds the run done and sends the original
// request again as a fresh request.
func TestRound5ClaimReleaseCannotDropAPendingDelivery(t *testing.T) {
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_r5_claim", "mock/model")}
	limit := vendorLimitError{provider.Limit{Kind: provider.LimitSubscriptionAllowance, Scope: provider.ScopeAccount, Connector: "claude",
		ResetAt: time.Now().Add(time.Hour), Message: "plan limit", Source: "vendor-frame"}}
	main := &scriptedAttemptsMain{attempts: []attemptScript{
		{closed: true, err: limit},
		{closed: true, err: limit},
		{closed: true, reply: "finished"},
		{closed: true, reply: "finished again"},
	}}
	out := &round5GateWriter{match: "unfinished provider conversation", gate: make(chan struct{})}
	deliveries := make(chan string, 4)
	var gateOnce sync.Once
	agent := New(Options{Backend: main, Model: "mock/model", Mode: ModeCode, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: out,
		ContinuityMode: "on",
		Switch:         func(context.Context, continuity.Candidate) (string, error) { return "", errors.New("no switch") },
		ResumeWait:     func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
		ProbeLimit:     func(context.Context, continuity.Pause) (bool, error) { return true, nil },
		ResumeReady: func(_ context.Context, pending string) bool {
			deliveries <- pending
			if out.armed.Load() {
				gateOnce.Do(func() { close(out.gate) })
			}
			return true // accepted; the surface runs it when the turn lock frees
		}})
	defer agent.Close()
	var paused *PausedError
	if err := agent.RunTurn(context.Background(), "deploy the service"); !errors.As(err, &paused) {
		t.Fatalf("setup: R0 not paused: %v", err)
	}
	stop := agent.WatchPauses(context.Background())
	defer stop()
	var first string
	select {
	case first = <-deliveries: // M0 delivered the first pause: claim X
	case <-time.After(5 * time.Second):
		t.Fatal("setup: the monitor never delivered the first pause")
	}
	out.armed.Store(true)
	// R1, the delivered turn, as the surface runs it.
	r1 := agent.RunTurn(context.Background(), first)
	t.Logf("R1 = %v; gate held=%v", r1, out.held.Load())
	var second string
	select {
	case second = <-deliveries:
	case <-time.After(5 * time.Second):
		t.Fatal("setup: no second delivery (monitor armed by ContinueOn never delivered)")
	}
	if !out.held.Load() {
		t.Log("note: the monitor did not deliver inside R1 (interleaving not forced)")
	}
	// The monitor's delivery of X is accepted and pending. /resume now:
	pending, ok := agent.Resume()
	t.Logf("pending delivery=%q; /resume ok=%v pending=%q", second, ok, pending)
	if !ok {
		return // no double delivery
	}
	// REPL order where /resume wins turnMu: /resume's turn, then the delivery.
	errA := agent.RunTurn(context.Background(), pending)
	errB := agent.RunTurn(context.Background(), second)
	main.mu.Lock()
	defer main.mu.Unlock()
	for i, sent := range main.sent {
		t.Logf("call %d ends with %q", i+1, sent[len(sent)-1].Content)
	}
	t.Fatalf("DOUBLE DELIVERY: the run was handed out twice (monitor + /resume); /resume turn err=%v, delivered turn err=%v, vendor calls=%d", errA, errB, main.calls)
}
