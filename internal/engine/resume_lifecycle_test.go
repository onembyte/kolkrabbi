package engine

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
)

func pausedLifecycleAgent(t *testing.T) (*Agent, continuity.Pause) {
	t.Helper()
	a := New(Options{Model: "test/model", Mode: ModeCode, Out: io.Discard,
		Sess: enginetest.NewFakeSession("test-session", "test/model")})
	p := continuity.Pause{Kind: "endpoint_capacity", Since: time.Now(),
		ResetAt: time.Now().Add(-time.Minute), PendingTurn: "keep this task"}
	a.Sess.SetPaused(&p)
	a.ProbeLimit = func(context.Context, continuity.Pause) (bool, error) { return true, nil }
	t.Cleanup(func() { _ = a.Close() })
	return a, p
}

func awaitResumeSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for resume lifecycle")
	}
}

func TestExpiredPauseRetainsPendingInputUntilExplicitResume(t *testing.T) {
	for _, policy := range []string{ResumeManual, "auto"} {
		t.Run(policy, func(t *testing.T) {
			a, p := pausedLifecycleAgent(t)
			a.ResumePolicy = policy
			if a.stillPaused() == nil || a.Sess.Paused() == nil {
				t.Fatal("expiry consumed the pause")
			}
			pending, ok := a.Resume()
			if !ok || pending != p.PendingTurn {
				t.Fatalf("pending input = %q, %v", pending, ok)
			}
		})
	}
}

func TestMissingResumeCallbackKeepsThePause(t *testing.T) {
	a, p := pausedLifecycleAgent(t)
	a.ResumeWait = func(context.Context, time.Duration) error { t.Error("started an unusable watcher"); return nil }
	a.WatchPauses(context.Background())
	if a.armResume() {
		t.Fatal("armed without a delivery callback")
	}
	if got := a.watchPause(context.Background(), p); got != nil {
		t.Fatal("nil callback was considered deliverable")
	}
	if got := a.Sess.Paused(); got == nil || got.PendingTurn != p.PendingTurn {
		t.Fatalf("lost pending input: %+v", got)
	}
}

func TestRepeatedPauseGetsAnotherWatcher(t *testing.T) {
	for _, asynchronous := range []bool{false, true} {
		t.Run(map[bool]string{false: "synchronous", true: "asynchronous"}[asynchronous], func(t *testing.T) {
			a, first := pausedLifecycleAgent(t)
			waiting := make(chan struct{}, 4)
			release := make(chan struct{})
			a.ResumeWait = func(ctx context.Context, _ time.Duration) error {
				waiting <- struct{}{}
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			a.ResumeReady = func(_ context.Context, pending string) bool {
				if pending != first.PendingTurn {
					t.Errorf("wrong pending input: %q", pending)
				}
				pauseAgain := func() {
					next := first
					next.Since = next.Since.Add(time.Second)
					a.Sess.SetPaused(&next)
					a.armResume()
				}
				if asynchronous {
					go pauseAgain()
				} else {
					pauseAgain()
				}
				return true
			}
			a.WatchPauses(context.Background())
			awaitResumeSignal(t, waiting)
			release <- struct{}{}
			awaitResumeSignal(t, waiting)
		})
	}
}

func TestResumeCallbackCanResumeANewPauseWithoutJoiningItself(t *testing.T) {
	a, p := pausedLifecycleAgent(t)
	delivered := make(chan struct{})
	a.ResumeWait = func(context.Context, time.Duration) error { return nil }
	a.ResumeReady = func(_ context.Context, _ string) bool {
		next := p
		next.Since = p.Since.Add(time.Second)
		next.PendingTurn = "next task"
		a.Sess.SetPaused(&next)
		pending, ok := a.Resume()
		if !ok || pending != next.PendingTurn {
			t.Errorf("reentrant resume = %q, %v", pending, ok)
		}
		close(delivered)
		return true
	}
	a.WatchPauses(context.Background())
	awaitResumeSignal(t, delivered)
}

func TestCloseCancelsAndJoinsResumeDelivery(t *testing.T) {
	a, p := pausedLifecycleAgent(t)
	entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	a.ResumeWait = func(context.Context, time.Duration) error { return nil }
	a.ResumeReady = func(ctx context.Context, _ string) bool {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-release
		return false
	}
	a.WatchPauses(context.Background())
	awaitResumeSignal(t, entered)
	closed := make(chan struct{})
	go func() { _ = a.Close(); close(closed) }()
	awaitResumeSignal(t, cancelled)
	select {
	case <-closed:
		t.Fatal("Close returned while delivery was running")
	default:
	}
	unblock()
	awaitResumeSignal(t, closed)
	if got := a.Sess.Paused(); got == nil || got.PendingTurn != p.PendingTurn {
		t.Fatalf("declined handoff lost pending input: %+v", got)
	}
	a.WatchPauses(context.Background())
	if a.armResume() {
		t.Fatal("closed agent restarted a watcher")
	}
}

func TestCancelledProbePreservesPendingInput(t *testing.T) {
	a, p := pausedLifecycleAgent(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	probed := make(chan struct{})
	a.ResumeWait = func(context.Context, time.Duration) error { return nil }
	a.ResumeReady = func(context.Context, string) bool { t.Error("delivered after cancellation"); return true }
	a.ProbeLimit = func(context.Context, continuity.Pause) (bool, error) {
		cancel()
		close(probed)
		return true, nil
	}
	a.WatchPauses(ctx)
	awaitResumeSignal(t, probed)
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if got := a.Sess.Paused(); got == nil || got.PendingTurn != p.PendingTurn {
		t.Fatalf("cancelled probe lost input: %+v", got)
	}
}

func TestDeclinedResumeRetainsANewerPause(t *testing.T) {
	a, first := pausedLifecycleAgent(t)
	next := first
	next.Since = first.Since.Add(time.Second)
	next.PendingTurn = "new pending task"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	a.ResumeWait = func(context.Context, time.Duration) error { return nil }
	a.ResumeReady = func(context.Context, string) bool {
		a.Sess.SetPaused(&next)
		cancel()
		close(entered)
		return false
	}
	a.WatchPauses(ctx)
	awaitResumeSignal(t, entered)
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if got := a.Sess.Paused(); got == nil || got.PendingTurn != next.PendingTurn {
		t.Fatalf("declined handoff replaced a newer pause: %+v", got)
	}
}

func TestDeclinedResumeRetriesWithBackoff(t *testing.T) {
	a, p := pausedLifecycleAgent(t)
	waits := make(chan time.Duration, 2)
	release, delivered := make(chan struct{}), make(chan struct{})
	var attempts int
	a.ResumeWait = func(ctx context.Context, delay time.Duration) error {
		waits <- delay
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	a.ResumeReady = func(context.Context, string) bool {
		attempts++
		if attempts == 1 {
			return false
		}
		close(delivered)
		return true
	}
	a.WatchPauses(context.Background())
	<-waits
	release <- struct{}{}
	select {
	case delay := <-waits:
		if delay < 29*time.Second {
			t.Fatalf("declined delivery retry did not back off: %v", delay)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("declined delivery was never retried")
	}
	if got := a.Sess.Paused(); got == nil || got.PendingTurn != p.PendingTurn {
		t.Fatalf("request was not retained while waiting: %+v", got)
	}
	release <- struct{}{}
	awaitResumeSignal(t, delivered)
}

func TestEmptyPauseLiftsWithoutSubmittingAnEmptyTurn(t *testing.T) {
	a, p := pausedLifecycleAgent(t)
	p.PendingTurn = " \t"
	a.Sess.SetPaused(&p)
	a.ResumeWait = func(context.Context, time.Duration) error { return nil }
	a.ResumeReady = func(context.Context, string) bool { t.Error("submitted an empty turn"); return true }
	a.WatchPauses(context.Background())
	a.resumeMu.Lock()
	monitor := a.resume
	a.resumeMu.Unlock()
	if monitor != nil {
		awaitResumeSignal(t, monitor.done)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if a.Sess.Paused() != nil {
		t.Fatal("empty pause did not lift")
	}
}

func TestContinueStopsTheWatcherBeforeSwitching(t *testing.T) {
	entered, canceled := make(chan struct{}), make(chan struct{})
	var out bytes.Buffer
	a := pausedAgent(t, &out, func(_ context.Context, c continuity.Candidate) (string, error) {
		select {
		case <-canceled:
		default:
			t.Error("switch started while the watcher could still claim the pause")
		}
		return c.Model, nil
	})
	a.Out = io.Discard
	t.Cleanup(func() { _ = a.Close() })
	a.ResumeWait = func(ctx context.Context, _ time.Duration) error {
		close(entered)
		<-ctx.Done()
		close(canceled)
		return ctx.Err()
	}
	a.ResumeReady = func(context.Context, string) bool { t.Error("duplicate delivery during switch"); return true }
	a.WatchPauses(context.Background())
	awaitResumeSignal(t, entered)
	if pending, _, err := a.ContinueOn(context.Background(), 0); err != nil || pending != "finish the report" {
		t.Fatalf("continue = %q, %v", pending, err)
	}
}

func TestStaleProbeRetiresAndWatchesTheNewPause(t *testing.T) {
	a, first := pausedLifecycleAgent(t)
	entered, release := make(chan struct{}), make(chan struct{})
	delivered := make(chan string, 1)
	a.ResumeWait = func(context.Context, time.Duration) error { return nil }
	a.ProbeLimit = func(ctx context.Context, p continuity.Pause) (bool, error) {
		if p.Since.Equal(first.Since) {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return false, ctx.Err()
			}
		}
		return true, nil
	}
	a.ResumeReady = func(_ context.Context, pending string) bool { delivered <- pending; return true }
	a.WatchPauses(context.Background())
	awaitResumeSignal(t, entered)
	next := first
	next.Since = first.Since.Add(time.Second)
	next.PendingTurn = "new pending task"
	a.Sess.SetPaused(&next)
	a.armResume()
	close(release)
	select {
	case pending := <-delivered:
		if pending != next.PendingTurn {
			t.Fatalf("stale probe delivered %q", pending)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("new pause was not watched")
	}
}

// A re-armed pause runs on kolk's own schedule. After a probe finds the cap
// still on, or a surface declines the delivery, the next time is kolk's guess,
// so it reads "retry at", even when the first time came from the vendor.
func TestARearmedPauseSaysRetryNotReset(t *testing.T) {
	t.Run("still capped", func(t *testing.T) {
		h := newResumeHarness(t, "", func(n int) bool { return n >= 2 })
		entered, release := make(chan struct{}, 2), make(chan struct{})
		h.a.ResumeWait = func(ctx context.Context, _ time.Duration) error {
			entered <- struct{}{}
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		h.pause(t)
		<-entered
		if p := h.a.Sess.Paused(); p == nil || p.Estimated || !strings.HasPrefix(p.RetryStatus(), "reset at ") {
			t.Fatalf("the vendor's Retry-After pause = %+v", p)
		}
		release <- struct{}{}
		<-entered
		if p := h.a.Sess.Paused(); p == nil || !p.Estimated || !strings.HasPrefix(p.RetryStatus(), "retry at ") {
			t.Fatalf("the re-armed pause = %+v; want kolk's own retry time", p)
		}
		release <- struct{}{}
		select {
		case <-h.resumed:
		case <-time.After(5 * time.Second):
			t.Fatal("the lifted cap never resumed the turn")
		}
	})
	t.Run("declined delivery", func(t *testing.T) {
		a, _ := pausedLifecycleAgent(t)
		entered, release := make(chan struct{}, 2), make(chan struct{})
		a.ResumeWait = func(ctx context.Context, _ time.Duration) error {
			entered <- struct{}{}
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		declined := false
		a.ResumeReady = func(context.Context, string) bool {
			if !declined {
				declined = true
				return false
			}
			return true
		}
		a.WatchPauses(context.Background())
		<-entered
		release <- struct{}{}
		<-entered
		if p := a.Sess.Paused(); p == nil || !p.Estimated || !strings.HasPrefix(p.RetryStatus(), "retry at ") {
			t.Fatalf("the backed-off pause = %+v; want kolk's own retry time", p)
		}
		release <- struct{}{}
	})
}
