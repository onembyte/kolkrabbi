package engine

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

const upstreamRateLimitBody = `{"error":{"message":"Provider returned error","metadata":{"provider_name":"Stealth","limit_source":"upstream_provider_shared_pool","remedy_hint":"Retry shortly"}}}`

type fakeRecorder struct {
	// mu because an orchestrated run records from several subagents at once,
	// which is the contract every Recorder now has to meet.
	mu      sync.Mutex
	Calls   []CallRecord
	Ratings []struct {
		Session string
		Turn    string
		Rating  int
	}
}

func (r *fakeRecorder) RecordCall(c CallRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Calls = append(r.Calls, c)
	return nil
}

func (r *fakeRecorder) RecordRating(session, turn string, rating int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Ratings = append(r.Ratings, struct {
		Session string
		Turn    string
		Rating  int
	}{Session: session, Turn: turn, Rating: rating})
	return nil
}

func newTestAgentInternal(t *testing.T, srv *enginetest.Server, mode string) (*Agent, *bytes.Buffer, *enginetest.FakeSession, *fakeRecorder) {
	t.Helper()
	client := provider.NewClient("test-key")
	client.BaseURL = srv.URL
	sess := enginetest.NewFakeSession("s_test", "mock/model")
	rec := &fakeRecorder{}
	var out bytes.Buffer
	ag := New(Options{
		Client: client, Model: "mock/model", Mode: mode, Permission: PermissionFullAuto,
		Sess: sess, Out: &out, Recorder: rec,
	})
	return ag, &out, sess, rec
}

func TestRateLimitRetriesIdenticalCodeRequestThenSucceeds(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{StatusCode: http.StatusTooManyRequests, ErrorBody: upstreamRateLimitBody},
		enginetest.Step{Text: "continued successfully"},
	)
	defer srv.Close()

	ag, out, _, rec := newTestAgentInternal(t, srv, ModeCode)
	activityLog := &activityEvents{}
	ag.Activity = &recordingActivity{events: activityLog}
	var waits []time.Duration
	ag.RetryWait = func(_ context.Context, delay time.Duration) error {
		waits = append(waits, delay)
		return nil
	}
	if err := ag.RunTurn(context.Background(), "continue the project"); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	if len(srv.Requests) != 2 || !messagesEqual(srv.Requests[0], srv.Requests[1]) {
		t.Fatalf("retry changed request history: %#v", srv.Requests)
	}
	if len(waits) != 1 || waits[0] != time.Second {
		t.Fatalf("retry waits = %v, want [1s]", waits)
	}
	if eventCount(activityLog.snapshot(), "start:thinking") != 1 || eventCount(activityLog.snapshot(), "stop:thinking") != 1 {
		t.Fatalf("retry split one logical activity: %#v", activityLog.snapshot())
	}
	if !strings.Contains(out.String(), "continued successfully") {
		t.Fatalf("successful retry output missing: %q", out.String())
	}
	assertSingleSavedAssistantAndCall(t, ag, rec)
}

func TestRateLimitRetryIsSharedByAgentPlanner(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{StatusCode: http.StatusTooManyRequests, ErrorBody: upstreamRateLimitBody},
		enginetest.Step{Text: `["one task"]`},
	)
	defer srv.Close()

	ag, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	ag.RetryWait = func(context.Context, time.Duration) error { return nil }
	tasks, _, err := ag.plan(context.Background(), "mock/model", "continue", 3)
	if err != nil || len(tasks) != 1 || tasks[0].Title != "one task" {
		t.Fatalf("planner retry = %v, %v", tasks, err)
	}
	if len(srv.Requests) != 2 || !messagesEqual(srv.Requests[0], srv.Requests[1]) {
		t.Fatalf("planner retry changed request: %#v", srv.Requests)
	}
}

func TestRateLimitRetryExhaustionIsBoundedAndActionable(t *testing.T) {
	steps := make([]enginetest.Step, 4)
	for i := range steps {
		steps[i] = enginetest.Step{StatusCode: http.StatusTooManyRequests, ErrorBody: upstreamRateLimitBody}
	}
	srv := enginetest.New(steps...)
	defer srv.Close()

	ag, _, _, _ := newTestAgentInternal(t, srv, ModeCode)
	var waits []time.Duration
	ag.RetryWait = func(_ context.Context, delay time.Duration) error {
		waits = append(waits, delay)
		return nil
	}
	err := ag.RunTurn(context.Background(), "continue")
	if err == nil || !strings.Contains(err.Error(), "mock/model") || !strings.Contains(err.Error(), "/model") {
		t.Fatalf("exhausted retry error = %v", err)
	}
	wantWaits := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
	if len(srv.Requests) != 4 || !durationsEqual(waits, wantWaits) {
		t.Fatalf("requests/waits = %d/%v, want 4/%v", len(srv.Requests), waits, wantWaits)
	}
}

func TestRateLimitRetryAfterHonorsOnlyBoundedServerDelay(t *testing.T) {
	t.Run("honor within cap", func(t *testing.T) {
		srv := enginetest.New(
			enginetest.Step{StatusCode: http.StatusTooManyRequests, RetryAfter: "3", ErrorBody: upstreamRateLimitBody},
			enginetest.Step{Text: "continued"},
		)
		defer srv.Close()
		ag, _, _, _ := newTestAgentInternal(t, srv, ModeCode)
		var waited time.Duration
		ag.RetryWait = func(_ context.Context, delay time.Duration) error { waited = delay; return nil }
		if err := ag.RunTurn(context.Background(), "continue"); err != nil {
			t.Fatal(err)
		}
		if waited != 3*time.Second || len(srv.Requests) != 2 {
			t.Fatalf("server-directed retry = %v with %d requests", waited, len(srv.Requests))
		}
	})

	t.Run("surface delay beyond cap", func(t *testing.T) {
		srv := enginetest.New(enginetest.Step{
			StatusCode: http.StatusTooManyRequests,
			RetryAfter: "30",
			ErrorBody:  upstreamRateLimitBody,
		})
		defer srv.Close()
		ag, _, _, _ := newTestAgentInternal(t, srv, ModeCode)
		waited := false
		ag.RetryWait = func(context.Context, time.Duration) error { waited = true; return nil }
		err := ag.RunTurn(context.Background(), "continue")
		if err == nil || !strings.Contains(err.Error(), "30s") || !strings.Contains(err.Error(), "/model") {
			t.Fatalf("long Retry-After error = %v", err)
		}
		if waited || len(srv.Requests) != 1 {
			t.Fatalf("long Retry-After waited=%v requests=%d", waited, len(srv.Requests))
		}
	})
}

func TestRateLimitRetryWaitIsCancellable(t *testing.T) {
	srv := enginetest.New(enginetest.Step{StatusCode: http.StatusTooManyRequests, ErrorBody: upstreamRateLimitBody})
	defer srv.Close()

	ag, _, _, _ := newTestAgentInternal(t, srv, ModeCode)
	ctx, cancel := context.WithCancel(context.Background())
	ag.RetryWait = func(waitCtx context.Context, _ time.Duration) error {
		cancel()
		<-waitCtx.Done()
		return waitCtx.Err()
	}
	err := ag.RunTurn(ctx, "continue")
	if !errors.Is(err, context.Canceled) || len(srv.Requests) != 1 {
		t.Fatalf("cancelled retry = %v with %d requests", err, len(srv.Requests))
	}
}

func TestRateLimitDoesNotRetryOtherOrMidStreamErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		step enginetest.Step
	}{
		{name: "server error", step: enginetest.Step{StatusCode: http.StatusServiceUnavailable, ErrorBody: `{"error":{"message":"unavailable"}}`}},
		{name: "stream error", step: enginetest.Step{StreamError: "rate limited after stream start"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := enginetest.New(tc.step)
			defer srv.Close()
			ag, _, _, _ := newTestAgentInternal(t, srv, ModeCode)
			waited := false
			ag.RetryWait = func(context.Context, time.Duration) error { waited = true; return nil }
			if err := ag.RunTurn(context.Background(), "continue"); err == nil {
				t.Fatal("expected provider error")
			}
			if len(srv.Requests) != 1 || waited {
				t.Fatalf("non-retriable error made %d requests; waited=%v", len(srv.Requests), waited)
			}
		})
	}
}

func messagesEqual(a, b []provider.Message) bool {
	return reflect.DeepEqual(a, b)
}

func durationsEqual(a, b []time.Duration) bool { return reflect.DeepEqual(a, b) }

func assertSingleSavedAssistantAndCall(t *testing.T, ag *Agent, rec *fakeRecorder) {
	t.Helper()
	msgs := ag.Sess.GetMessages()
	if len(msgs) != 3 || msgs[2].Role != "assistant" {
		t.Fatalf("saved retry history = %#v", msgs)
	}
	if len(rec.Calls) != 1 {
		t.Fatalf("accounted provider calls = %d, want one successful call", len(rec.Calls))
	}
}
