package cli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/session"
)

type fakeAnimationTimer struct {
	delay time.Duration
	c     chan time.Time
}

func (t *fakeAnimationTimer) C() <-chan time.Time { return t.c }
func (t *fakeAnimationTimer) Stop()               {}
func (t *fakeAnimationTimer) fire()               { t.c <- time.Time{} }

type fakeAnimationClock struct {
	created chan *fakeAnimationTimer
}

func newFakeAnimationClock() *fakeAnimationClock {
	return &fakeAnimationClock{created: make(chan *fakeAnimationTimer)}
}

func (c *fakeAnimationClock) NewTimer(delay time.Duration) animationTimer {
	timer := &fakeAnimationTimer{delay: delay, c: make(chan time.Time, 1)}
	c.created <- timer
	return timer
}

func nextAnimationTimer(t *testing.T, clock *fakeAnimationClock, want time.Duration) *fakeAnimationTimer {
	t.Helper()
	select {
	case timer := <-clock.created:
		if timer.delay != want {
			t.Fatalf("animation delay = %v, want %v", timer.delay, want)
		}
		return timer
	case <-time.After(time.Second):
		t.Fatal("animation did not request its next timer")
		return nil
	}
}

func TestOctopusActivityUsesGraceFramesAndExactCleanup(t *testing.T) {
	clock := newFakeAnimationClock()
	var out bytes.Buffer
	activity := &octopusActivity{
		out: outWriter{Writer: &out}, clock: clock, color: true,
		grace: 120 * time.Millisecond, interval: 120 * time.Millisecond,
	}
	stop := activity.Start(context.Background(), "thinking")

	first := nextAnimationTimer(t, clock, 120*time.Millisecond)
	if out.Len() != 0 {
		t.Fatalf("animation rendered during grace: %q", out.String())
	}
	first.fire()
	second := nextAnimationTimer(t, clock, 120*time.Millisecond)
	wantFirst := "\x1b[s\x1b[95m⠋ 🐙\x1b[0m thinking…"
	if out.String() != wantFirst {
		t.Fatalf("first frame = %q, want %q", out.String(), wantFirst)
	}
	second.fire()
	_ = nextAnimationTimer(t, clock, 120*time.Millisecond)
	wantSecond := wantFirst + "\x1b[u\x1b[K\x1b[95m⠙ 🐙\x1b[0m thinking…"
	if out.String() != wantSecond {
		t.Fatalf("second frame = %q, want %q", out.String(), wantSecond)
	}
	stop()
	stop() // the engine and renderer may both guard cleanup; it stays idempotent
	if got := out.String(); got != wantSecond+"\x1b[u\x1b[K" {
		t.Fatalf("cleaned animation = %q", got)
	}
}

func TestOctopusActivityFastAndCancelledPathsLeaveNoFrame(t *testing.T) {
	t.Run("fast response", func(t *testing.T) {
		clock := newFakeAnimationClock()
		var out bytes.Buffer
		activity := &octopusActivity{out: outWriter{Writer: &out}, clock: clock, color: true, grace: 120 * time.Millisecond, interval: 120 * time.Millisecond}
		stop := activity.Start(context.Background(), "thinking")
		_ = nextAnimationTimer(t, clock, 120*time.Millisecond)
		stop()
		if out.Len() != 0 {
			t.Fatalf("fast response left terminal bytes: %q", out.String())
		}
	})

	t.Run("cancel after render", func(t *testing.T) {
		clock := newFakeAnimationClock()
		var out bytes.Buffer
		activity := &octopusActivity{out: outWriter{Writer: &out}, clock: clock, color: true, grace: 120 * time.Millisecond, interval: 120 * time.Millisecond}
		ctx, cancel := context.WithCancel(context.Background())
		stop := activity.Start(ctx, "planning")
		first := nextAnimationTimer(t, clock, 120*time.Millisecond)
		first.fire()
		_ = nextAnimationTimer(t, clock, 120*time.Millisecond)
		cancel()
		stop() // joins the context-owned renderer
		if got := out.String(); !strings.HasSuffix(got, "\x1b[u\x1b[K") || strings.Count(got, "\x1b[s") != 1 {
			t.Fatalf("cancelled frame was not restored exactly once: %q", got)
		}
	})
}

func TestOctopusActivityHonorsNoColorWithoutRemovingStatus(t *testing.T) {
	clock := newFakeAnimationClock()
	var out bytes.Buffer
	activity := &octopusActivity{out: outWriter{Writer: &out}, clock: clock, color: false, grace: 120 * time.Millisecond, interval: 120 * time.Millisecond}
	stop := activity.Start(context.Background(), "synthesizing")
	first := nextAnimationTimer(t, clock, 120*time.Millisecond)
	first.fire()
	_ = nextAnimationTimer(t, clock, 120*time.Millisecond)
	stop()
	if got := out.String(); !strings.Contains(got, "⠋ 🐙 synthesizing…") || strings.Contains(got, "\x1b[95m") {
		t.Fatalf("no-color status = %q", got)
	}
}

type inertActivity struct{}

func (inertActivity) Start(context.Context, string) func() { return func() {} }

func TestAttachInteractiveActivityRequiresReplAndTerminal(t *testing.T) {
	ag := engine.New(engine.Options{Sess: session.New(t.TempDir(), "mock/model"), Out: io.Discard})
	factoryCalls := 0
	a := &app{
		stdout:     io.Discard,
		canAnimate: func() bool { return true },
		newActivity: func(io.Writer) engine.ActivityIndicator {
			factoryCalls++
			return inertActivity{}
		},
	}

	a.attachInteractiveActivity(ag, false)
	if factoryCalls != 0 || ag.Activity != nil {
		t.Fatal("single-shot run enabled terminal activity")
	}
	a.canAnimate = func() bool { return false }
	a.attachInteractiveActivity(ag, true)
	if factoryCalls != 0 || ag.Activity != nil {
		t.Fatal("non-terminal REPL enabled terminal activity")
	}
	a.canAnimate = func() bool { return true }
	a.attachInteractiveActivity(ag, true)
	if factoryCalls != 1 || ag.Activity == nil {
		t.Fatal("interactive terminal REPL did not enable activity")
	}
}

// outWriter makes the writer field comparable in compiler errors and keeps
// test construction explicit about the renderer's only output dependency.
type outWriter struct{ io.Writer }
