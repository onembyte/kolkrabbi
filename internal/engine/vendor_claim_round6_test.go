package engine

// Found by the round-6 vendor-recovery verifier: the claim on a delivered run.
// The delivered turn runs in the context the engine handed the surface's
// callback, as the REPL runs it (repl.go: resumeCtx -> runInteractivePrompt ->
// RunTurn), so the delivery's own claim travels with it.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

// R6-C1. The plain REPL runs a delivered turn on the monitor's goroutine,
// waiting on turnMu. The monitor has already lifted the pause and claimed
// the run. A typed request that takes turnMu first (typed ahead, or a piped
// script) passes stillPaused (the pause is gone) and is refused by
// restoreExecution ("an unfinished request is saved") — and that refusal
// clears the claim unconditionally, the pending delivery's included. A
// /resume that also beats the delivery to turnMu then hands the run out a
// second time: the run is continued once by /resume, and the delivery then
// finds it done and sends the original request again as a fresh request.
func TestRound6RefusedTypedTurnDropsAPendingDeliveryClaim(t *testing.T) {
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_r6_claim", "mock/model")}
	limit := vendorLimitError{provider.Limit{Kind: provider.LimitSubscriptionAllowance, Scope: provider.ScopeAccount, Connector: "claude",
		ResetAt: time.Now().Add(time.Hour), Message: "plan limit", Source: "vendor-frame"}}
	main := &scriptedAttemptsMain{attempts: []attemptScript{
		{closed: true, err: limit},
		{closed: true, reply: "finished"},
		{closed: true, reply: "finished again"},
	}}
	// The REPL's callback waits for the turn lock and then runs the delivered
	// turn itself, in the context it was handed; proceed stands for the lock.
	deliveries, proceed, delivered := make(chan string, 4), make(chan struct{}), make(chan error, 4)
	var agent *Agent
	agent = New(Options{Backend: main, Model: "mock/model", Mode: ModeCode, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: &strings.Builder{},
		ResumeWait: func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
		ProbeLimit: func(context.Context, continuity.Pause) (bool, error) { return true, nil },
		ResumeReady: func(ctx context.Context, pending string) bool {
			deliveries <- pending
			<-proceed
			delivered <- agent.RunTurn(ctx, pending)
			return true
		}})
	defer agent.Close()
	var paused *PausedError
	if err := agent.RunTurn(context.Background(), "deploy the service"); !errors.As(err, &paused) {
		t.Fatalf("setup: not paused: %v", err)
	}
	stop := agent.WatchPauses(context.Background())
	defer stop()
	var pendingInput string
	select {
	case pendingInput = <-deliveries:
	case <-time.After(5 * time.Second):
		t.Fatal("setup: the monitor never delivered")
	}
	_ = pendingInput
	// A typed request wins turnMu while the delivery waits for it.
	typedErr := agent.RunTurn(context.Background(), "what is the status?")
	t.Logf("typed turn: %v", typedErr)
	// So does /resume.
	pending, ok := agent.Resume()
	t.Logf("/resume ok=%v pending=%q (delivery pending=%q)", ok, pending, pendingInput)
	if !ok {
		// The pending delivery's claim held: the delivery, when it gets the
		// turn lock, continues the run once.
		close(proceed)
		if err := <-delivered; err != nil {
			t.Fatalf("the delivered turn: %v", err)
		}
		main.mu.Lock()
		defer main.mu.Unlock()
		if main.calls != 2 || !strings.Contains(main.sent[1][len(main.sent[1])-1].Content, "Continue this unfinished request") {
			t.Fatalf("the delivery made %d calls; want one continuation of the saved request", main.calls)
		}
		return
	}
	errA := agent.RunTurn(context.Background(), pending)
	close(proceed) // the delivery finally gets turnMu
	errB := <-delivered
	main.mu.Lock()
	defer main.mu.Unlock()
	for i, sent := range main.sent {
		t.Logf("vendor call %d ends with %q", i+1, sent[len(sent)-1].Content)
	}
	t.Fatalf("DOUBLE DELIVERY: a refused typed request released the pending delivery's claim; /resume handed the run out again "+
		"(/resume turn err=%v, delivered turn err=%v, vendor calls=%d)", errA, errB, main.calls)
}

// R6-C1b. The same pending REPL delivery; the request typed again verbatim
// wins turnMu first. That turn restores the run (restoreExecution does not
// consult the claim), continues and finishes it, and its release clears the
// delivery's claim (the count has not moved since it restored). The delivery
// then runs: the run is done, so its input starts a fresh request and the
// original request is sent again on the same vendor conversation.
func TestRound6RetypedRequestThenPendingDeliveryReplays(t *testing.T) {
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_r6_claim_b", "mock/model")}
	limit := vendorLimitError{provider.Limit{Kind: provider.LimitSubscriptionAllowance, Scope: provider.ScopeAccount, Connector: "claude",
		ResetAt: time.Now().Add(time.Hour), Message: "plan limit", Source: "vendor-frame"}}
	main := &scriptedAttemptsMain{attempts: []attemptScript{
		{closed: true, err: limit},
		{closed: true, reply: "finished"},
		{closed: true, reply: "finished again"},
	}}
	// The REPL's callback waits for the turn lock and then runs the delivered
	// turn itself, in the context it was handed; proceed stands for the lock.
	deliveries, proceed, delivered := make(chan string, 4), make(chan struct{}), make(chan error, 4)
	var agent *Agent
	agent = New(Options{Backend: main, Model: "mock/model", Mode: ModeCode, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: &strings.Builder{},
		ResumeWait: func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
		ProbeLimit: func(context.Context, continuity.Pause) (bool, error) { return true, nil },
		ResumeReady: func(ctx context.Context, pending string) bool {
			deliveries <- pending
			<-proceed
			delivered <- agent.RunTurn(ctx, pending)
			return true
		}})
	defer agent.Close()
	var paused *PausedError
	if err := agent.RunTurn(context.Background(), "deploy the service"); !errors.As(err, &paused) {
		t.Fatalf("setup: not paused: %v", err)
	}
	stop := agent.WatchPauses(context.Background())
	defer stop()
	var pendingInput string
	select {
	case pendingInput = <-deliveries:
	case <-time.After(5 * time.Second):
		t.Fatal("setup: the monitor never delivered")
	}
	_ = pendingInput
	typed := agent.RunTurn(context.Background(), "deploy the service") // typed again, wins turnMu
	close(proceed)                                                     // the delivery gets turnMu next
	pendingDelivery := <-delivered
	main.mu.Lock()
	defer main.mu.Unlock()
	for i, sent := range main.sent {
		t.Logf("vendor call %d ends with %.80q", i+1, sent[len(sent)-1].Content)
	}
	t.Logf("typed=%v delivery=%v calls=%d", typed, pendingDelivery, main.calls)
	for i := 1; i < len(main.sent); i++ {
		if last := main.sent[i][len(main.sent[i])-1].Content; last == "deploy the service" {
			t.Fatalf("REPLAYED: vendor call %d sends the original request again after the run was continued and finished", i+1)
		}
	}
	if typed != nil || pendingDelivery != nil || main.calls != 2 {
		t.Fatalf("typed=%v delivery=%v calls=%d; want the run continued once and the stale delivery skipped", typed, pendingDelivery, main.calls)
	}
}
