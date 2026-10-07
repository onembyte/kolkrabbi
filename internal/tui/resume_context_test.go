package tui

import (
	"context"
	"io"
	"testing"
	"time"
)

func TestAutomaticContinuationKeepsDeliveryValuesUnderSessionLifetime(t *testing.T) {
	type claimKey struct{}
	session, stopSession := context.WithCancel(context.Background())
	defer stopSession()
	delivery, stopDelivery := context.WithCancel(context.WithValue(session, claimKey{}, 17))
	started := make(chan context.Context, 1)
	finished := make(chan struct{})
	r := NewRuntime(RuntimeOptions{Output: io.Discard, Turn: func(ctx context.Context, _ string) error {
		started <- ctx
		<-ctx.Done()
		close(finished)
		return ctx.Err()
	}})
	r.baseContext = session
	if !r.SubmitWhenIdleContext(delivery, "continue saved work") {
		t.Fatal("idle session refused continuation")
	}
	var turn context.Context
	select {
	case turn = <-started:
	case <-time.After(time.Second):
		t.Fatal("turn never started")
	}
	if turn.Value(claimKey{}) != 17 {
		t.Fatal("continuation dropped its delivery claim")
	}
	stopDelivery()
	if turn.Err() != nil {
		t.Fatal("returning monitor cancelled accepted work")
	}
	stopSession()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("session shutdown did not cancel work")
	}
	r.turns.Wait()
}

func TestCancelledContinuationCannotClaimAnIdleSurface(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := NewRuntime(RuntimeOptions{Output: io.Discard, Turn: func(context.Context, string) error { t.Error("cancelled delivery ran"); return nil }})
	r.baseContext = context.Background()
	if r.SubmitWhenIdleContext(ctx, "old delivery") || r.Snapshot().Transcript != "" {
		t.Fatal("cancelled delivery was accepted")
	}
}
