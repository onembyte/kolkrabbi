package engine

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/protocol"
)

// resumeHarness pauses a session on a 429 and records what the monitor does
// next: every wait it asks for, every probe, and the turn it hands back.
type resumeHarness struct {
	a       *Agent
	waits   chan time.Duration
	resumed chan string
	probes  int
	mu      sync.Mutex
}

func newResumeHarness(t *testing.T, policy string, probe func(n int) bool) *resumeHarness {
	t.Helper()
	srv := enginetest.New(enginetest.Step{StatusCode: http.StatusTooManyRequests, RetryAfter: "1800", ErrorBody: `{"error":{"message":"rate limited"}}`})
	t.Cleanup(srv.Close)
	h := &resumeHarness{waits: make(chan time.Duration, 8), resumed: make(chan string, 1)}
	h.a = New(Options{
		Client: provider.NewCompatibleClient(srv.URL), Bus: newTestBus(t), Mode: ModeCode, Model: "vendor/paid",
		OnSubscriptionLimit: OnLimitStop, ResumePolicy: policy,
		Permission: PermissionFullAuto, Out: io.Discard, Sess: enginetest.NewFakeSession("s_test", "vendor/paid"),
		ResumeWait: func(ctx context.Context, d time.Duration) error {
			h.waits <- d
			return ctx.Err()
		},
		ProbeLimit: func(context.Context, continuity.Pause) (bool, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.probes++
			return probe(h.probes), nil
		},
		ResumeReady: func(pending string) { h.resumed <- pending },
	})
	h.a.WatchPauses(context.Background())
	t.Cleanup(func() { _ = h.a.Close() })
	return h
}

func (h *resumeHarness) pause(t *testing.T) {
	t.Helper()
	err := h.a.RunTurn(context.Background(), "hello")
	var paused *PausedError
	if !errors.As(err, &paused) {
		t.Fatalf("a 429 with Retry-After did not pause: %v", err)
	}
}

func (h *resumeHarness) resumeEvents(t *testing.T) int {
	t.Helper()
	n := 0
	for _, env := range bReplay(t, h.a.Bus) {
		if env.Type != protocol.EventProviderLimit {
			continue
		}
		var data protocol.ProviderLimitData
		_ = json.Unmarshal(env.Data, &data)
		if data.Action == "resume" {
			n++
		}
	}
	return n
}

func TestAPausedTurnComesBackOnItsOwnWhenTheLimitLifts(t *testing.T) {
	h := newResumeHarness(t, "", func(int) bool { return true })
	h.pause(t)
	select {
	case pending := <-h.resumed:
		if pending != "hello" {
			t.Fatalf("resumed turn = %q, want the one that was waiting", pending)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nothing brought the paused turn back")
	}
	if h.a.Sess.Paused() != nil {
		t.Fatal("the session is still paused after the monitor resumed it")
	}
	if got := h.resumeEvents(t); got != 1 {
		t.Fatalf("resume events = %d, want exactly one", got)
	}
}

func TestAMonitorThatFindsTheCapStillOnBacksOffToTheNextReset(t *testing.T) {
	h := newResumeHarness(t, "", func(n int) bool { return n >= 2 })
	before := time.Now()
	h.pause(t)
	first := <-h.waits
	if first < 29*time.Minute || first > 31*time.Minute {
		t.Fatalf("first wait = %v, want the Retry-After (about 30 minutes)", first)
	}
	second := <-h.waits
	if second < 30*time.Second {
		t.Fatalf("second wait = %v, want the kind's default cooldown once the probe said still capped", second)
	}
	select {
	case <-h.resumed:
	case <-time.After(5 * time.Second):
		t.Fatal("the second probe said lifted and nothing came back")
	}
	h.mu.Lock()
	probes := h.probes
	h.mu.Unlock()
	if probes != 2 {
		t.Fatalf("probes = %d, want one per wait", probes)
	}
	if p := h.a.Sess.Paused(); p != nil && p.ResetAt.Before(before) {
		t.Fatal("the pause was not moved to the next reset")
	}
}

func TestManualResumeKeepsThePauseForSlashResume(t *testing.T) {
	h := newResumeHarness(t, "manual", func(int) bool { return true })
	h.pause(t)
	select {
	case <-h.resumed:
		t.Fatal("continuity.resume manual still resumed on its own")
	case <-time.After(200 * time.Millisecond):
	}
	pending, ok := h.a.Resume()
	if !ok || pending != "hello" {
		t.Fatalf("/resume gave (%q, %v), want the waiting turn", pending, ok)
	}
	if h.a.Sess.Paused() != nil {
		t.Fatal("/resume left the pause in place")
	}
	if _, ok := h.a.Resume(); ok {
		t.Fatal("a second /resume found a turn to resume")
	}
}

func TestTheResumeMonitorDiesWithTheAgent(t *testing.T) {
	released := make(chan struct{})
	h := newResumeHarness(t, "", func(int) bool { return true })
	h.a.ResumeWait = func(ctx context.Context, _ time.Duration) error {
		<-ctx.Done()
		close(released)
		return ctx.Err()
	}
	h.pause(t)
	if err := h.a.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("closing the agent left the monitor waiting")
	}
	select {
	case <-h.resumed:
		t.Fatal("a closed agent still handed a turn back")
	default:
	}
}
