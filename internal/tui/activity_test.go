package tui

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func activityRow(r *Runtime) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.controller.screen.activity
}

// Agent mode runs several subagents at once. The first one to finish must not
// blank the row, because the others are still working and a blank row reads as
// a frozen session -- the bug reported from agent mode.
func TestFinishingOneOfSeveralActivitiesKeepsTheRow(t *testing.T) {
	r := NewRuntime(RuntimeOptions{})
	ctx := context.Background()
	stopA := r.Start(ctx, "thinking")
	stopB := r.StartWork(ctx, "grep")
	stopC := r.StartWork(ctx, "read")

	stopC()
	if row := activityRow(r); row == "" {
		t.Fatal("the row went blank while two activities were still running")
	}
	stopB()
	if row := activityRow(r); row == "" {
		t.Fatal("the row went blank while one activity was still running")
	}
	if row := activityRow(r); !strings.Contains(row, "thinking") {
		t.Errorf("row fell back to %q, want the surviving activity's phase", row)
	}
	stopA()
	if row := activityRow(r); row != "" {
		t.Errorf("row is %q after the last activity stopped, want empty", row)
	}
}

// The newest activity is the most specific thing the session is doing, so it is
// what the row shows.
func TestTheNewestActivityOwnsTheRow(t *testing.T) {
	r := NewRuntime(RuntimeOptions{})
	ctx := context.Background()
	stopA := r.Start(ctx, "thinking")
	defer stopA()
	if row := activityRow(r); !strings.Contains(row, "thinking") {
		t.Fatalf("row is %q, want thinking", row)
	}
	stopB := r.StartWork(ctx, "bash")
	if row := activityRow(r); !strings.Contains(row, "working") {
		t.Errorf("row is %q, want the newer activity", row)
	}
	stopB()
	if row := activityRow(r); !strings.Contains(row, "thinking") {
		t.Errorf("row is %q after the newer one ended, want the older one back", row)
	}
}

// Stopping the same activity twice is what a deferred stop plus an explicit one
// looks like, and it must not disturb work that is still running.
func TestStoppingAnActivityTwiceIsHarmless(t *testing.T) {
	r := NewRuntime(RuntimeOptions{})
	ctx := context.Background()
	stopA := r.Start(ctx, "thinking")
	defer stopA()
	stopB := r.StartWork(ctx, "bash")
	stopB()
	stopB()
	if row := activityRow(r); !strings.Contains(row, "thinking") {
		t.Errorf("row is %q, want the still-running activity", row)
	}
}

// A cancelled parent context retires the activity without anyone calling stop,
// which is what happens when a turn is interrupted.
func TestCancellingTheContextRetiresTheActivity(t *testing.T) {
	r := NewRuntime(RuntimeOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	stop := r.Start(ctx, "thinking")
	cancel()
	deadline := time.Now().Add(2 * time.Second)
	for activityRow(r) != "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if row := activityRow(r); row != "" {
		t.Errorf("row is %q after the context was cancelled, want empty", row)
	}
	stop()
}

// The animator is shared, so it has to survive the list emptying and filling
// again. Run it under -race: this is where a handoff bug would show up.
func TestActivitiesChurnWithoutLosingTheAnimator(t *testing.T) {
	r := NewRuntime(RuntimeOptions{})
	ctx := context.Background()
	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				stop := r.StartWork(ctx, "tool")
				time.Sleep(time.Duration(worker%3) * time.Millisecond)
				stop()
			}
		}()
	}
	wg.Wait()
	if row := activityRow(r); row != "" {
		t.Errorf("row is %q once every activity finished, want empty", row)
	}
	// The animator must have retired rather than been left spinning on nothing.
	r.mu.Lock()
	animating := r.animating
	r.mu.Unlock()
	if animating {
		t.Error("the animator is still running with no activities left")
	}
}

// The animator has to stand down the moment the last activity ends rather than
// on its next frame. A stop that joins it would otherwise wait out a whole
// interval -- and against an injected clock that is not ticking, forever, which
// is how this first showed up: as a test that hung instead of failing.
func TestStoppingTheLastActivityDoesNotWaitForATick(t *testing.T) {
	r := NewRuntime(RuntimeOptions{Output: io.Discard})
	r.spinClock = newFakeSpinnerClock()
	stop := r.StartWork(context.Background(), "bash")

	returned := make(chan struct{})
	go func() { defer close(returned); stop() }()
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("stopping the last activity blocked on a clock that never ticked")
	}
	if row := activityRow(r); row != "" {
		t.Errorf("row is %q after the last stop, want empty", row)
	}
}
