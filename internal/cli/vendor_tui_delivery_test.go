package cli

import (
	"context"
	"errors"
	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/tui"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Drive the real adapter, decorator, resume monitor and asynchronous Runtime.
// A typed request wins while the already-claimed delivery waits for the screen.
func TestIndependentTUIRuntimeDoesNotReplayAStaleVendorDelivery(t *testing.T) {
	_, requests, side := round5Setup(t)
	sess := enginetest.NewFakeSession("s_tui_vendor_delivery", "claude-opus")
	sess.SetProviderStateName("H-saved")
	sess.SetAutoTitle("vendor resume")
	var ag *engine.Agent
	ag = engine.New(engine.Options{Backend: round5Decorated(t, sess, &ag), Model: "claude-opus", Mode: engine.ModeCode, Effort: engine.EffortHigh,
		Permission: engine.PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard,
		ResumeWait: func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
		ProbeLimit: func(context.Context, continuity.Pause) (bool, error) { return true, nil }})
	defer ag.Close()
	input := "deploy LIMIT service"
	var paused *engine.PausedError
	if err := ag.RunTurn(context.Background(), input); !errors.As(err, &paused) {
		t.Fatalf("setup: %v", err)
	}

	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	runtimeCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	claimed, proceed := make(chan struct{}), make(chan struct{})
	accepted := make(chan bool, 1)
	var callbacks atomic.Int32
	finished := make(chan error, 2)
	runtimeDone := make(chan error, 1)
	var screen *tui.Runtime
	stopResume := func() {}
	defer func() { stopResume() }()
	screen = tui.NewRuntime(tui.RuntimeOptions{Input: reader, Output: io.Discard,
		Ready: func(ctx context.Context) {
			ag.ResumeReady = func(delivery context.Context, pending string) bool {
				if callbacks.Add(1) > 1 {
					return false
				}
				close(claimed)
				select {
				case <-proceed:
				case <-delivery.Done():
					return false
				}
				// This is the production tuiRepl binding, with a scheduling barrier.
				ok := screen.SubmitWhenIdleContext(delivery, pending)
				accepted <- ok
				return ok
			}
			stopResume = ag.WatchPauses(ctx)
		},
		Turn: func(ctx context.Context, prompt string) error {
			err := ag.RunTurn(ctx, prompt)
			finished <- err
			return err
		}})
	go func() { runtimeDone <- screen.Run(runtimeCtx) }()
	select {
	case <-claimed:
	case <-time.After(5 * time.Second):
		t.Fatal("monitor not ready")
	}
	if !screen.Submit(input) {
		t.Fatal("typed request refused")
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("typed resume: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("typed resume timed out")
	}
	deadline := time.Now().Add(5 * time.Second)
	for screen.Snapshot().Status.Lifecycle != "ready" {
		if time.Now().After(deadline) {
			t.Fatal("screen did not become idle")
		}
		time.Sleep(time.Millisecond)
	}
	close(proceed)
	select {
	case ok := <-accepted:
		if !ok {
			t.Fatal("idle screen refused delivery")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("delivery not accepted")
	}
	var deliveryErr error
	select {
	case err := <-finished:
		deliveryErr = err
	case <-time.After(5 * time.Second):
		t.Fatal("stale delivery timed out")
	}
	cancel()
	select {
	case <-runtimeDone:
	case <-time.After(5 * time.Second):
		t.Fatal("runtime did not stop")
	}
	lines := round4Lines(requests)
	effects := round4Lines(side)
	t.Logf("vendor requests=%d effects=%q stale delivery=%v", len(lines), effects, deliveryErr)
	if len(lines) != 2 || len(effects) != 2 {
		t.Fatalf("REPLAY: got %d vendor requests and %d effects, want original + one continuation", len(lines), len(effects))
	}
	if !strings.Contains(lines[1], "Continue this unfinished request") {
		t.Fatal("saved vendor request was not continued")
	}
	if effects[1] != "continued in H-saved" {
		t.Fatalf("continuation moved: %q", effects[1])
	}
	if deliveryErr != nil {
		t.Fatalf("stale delivery: %v", deliveryErr)
	}
}
