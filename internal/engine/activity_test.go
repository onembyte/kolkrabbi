package engine

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

type activityEvents struct {
	mu     sync.Mutex
	events []string
}

func (e *activityEvents) add(event string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, event)
}

func (e *activityEvents) snapshot() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.events...)
}

func (e *activityEvents) Write(p []byte) (int, error) {
	e.add("write:" + string(p))
	return len(p), nil
}

type recordingActivity struct {
	events *activityEvents
}

func (a *recordingActivity) Start(ctx context.Context, phase string) func() {
	a.events.add("start:" + phase)
	if ctx.Err() != nil {
		a.events.add("context-cancelled")
	}
	return func() { a.events.add("stop:" + phase) }
}

func TestActivityStopsBeforeFirstVisibleToken(t *testing.T) {
	srv := enginetest.New(enginetest.Step{Text: "visible answer"})
	defer srv.Close()

	ag, _, _, _ := newTestAgentInternal(t, srv, ModeCode)
	events := &activityEvents{}
	ag.Out = events
	ag.Activity = &recordingActivity{events: events}
	if err := ag.RunTurn(context.Background(), "answer"); err != nil {
		t.Fatal(err)
	}
	got := events.snapshot()
	start := eventIndex(got, "start:thinking")
	stop := eventIndex(got, "stop:thinking")
	content := eventIndexContaining(got, "visible")
	if start < 0 || stop <= start || content <= stop {
		t.Fatalf("activity/token order = %#v", got)
	}
	if eventCount(got, "start:thinking") != 1 || eventCount(got, "stop:thinking") != 1 {
		t.Fatalf("activity lifecycle was not exactly once: %#v", got)
	}
}

func TestActivityStopsBeforeToolHandlingAndErrors(t *testing.T) {
	t.Run("tool-only response", func(t *testing.T) {
		srv := enginetest.New(
			enginetest.Step{ToolCalls: []provider.ToolCall{{
				ID: "call_read",
				Function: provider.FunctionCall{
					Name:      "read_file",
					Arguments: `{"path":"missing-for-activity-test"}`,
				},
			}}},
			enginetest.Step{Text: "finished"},
		)
		defer srv.Close()
		ag, _, _, _ := newTestAgentInternal(t, srv, ModeCode)
		events := &activityEvents{}
		ag.Out = events
		ag.Activity = &recordingActivity{events: events}
		if err := ag.RunTurn(context.Background(), "read"); err != nil {
			t.Fatal(err)
		}
		got := events.snapshot()
		if stop, tool := eventIndex(got, "stop:thinking"), eventIndexContaining(got, "→"); stop < 0 || tool <= stop {
			t.Fatalf("activity/tool order = %#v", got)
		}
		if eventCount(got, "start:thinking") != 2 || eventCount(got, "stop:thinking") != 2 {
			t.Fatalf("tool loop lifecycles = %#v", got)
		}
	})

	t.Run("provider error", func(t *testing.T) {
		srv := enginetest.New(enginetest.Step{StatusCode: http.StatusServiceUnavailable, ErrorBody: `{"error":{"message":"unavailable"}}`})
		defer srv.Close()
		ag, _, _, _ := newTestAgentInternal(t, srv, ModeCode)
		events := &activityEvents{}
		ag.Out = events
		ag.Activity = &recordingActivity{events: events}
		if err := ag.RunTurn(context.Background(), "answer"); err == nil {
			t.Fatal("expected provider error")
		}
		got := events.snapshot()
		if eventCount(got, "start:thinking") != 1 || eventCount(got, "stop:thinking") != 1 || got[len(got)-1] != "write:\n" {
			// runLoop prints its trailing newline only after streamChat has stopped.
			t.Fatalf("error lifecycle = %#v", got)
		}
		if eventIndex(got, "stop:thinking") >= len(got)-1 {
			t.Fatalf("error returned before activity stopped: %#v", got)
		}
	})
}

func TestAgentActivityPhasesAreDeterministic(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{Text: `["first task","second task"]`},
		enginetest.Step{Text: "first done"},
		enginetest.Step{Text: "second done"},
		enginetest.Step{Text: "all done"},
	)
	defer srv.Close()

	ag, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	events := &activityEvents{}
	ag.Out = events
	ag.Activity = &recordingActivity{events: events}
	if err := ag.RunTurn(context.Background(), "do both"); err != nil {
		t.Fatal(err)
	}
	var starts []string
	for _, event := range events.snapshot() {
		if strings.HasPrefix(event, "start:") {
			starts = append(starts, strings.TrimPrefix(event, "start:"))
		}
	}
	want := []string{"planning", "working", "working", "synthesizing"}
	if strings.Join(starts, ",") != strings.Join(want, ",") {
		t.Fatalf("activity phases = %v, want %v", starts, want)
	}
}

func TestCancelledContextReachesAndStopsActivity(t *testing.T) {
	srv := enginetest.New(enginetest.Step{Text: "unused"})
	defer srv.Close()
	ag, _, _, _ := newTestAgentInternal(t, srv, ModeCode)
	events := &activityEvents{}
	ag.Out = events
	ag.Activity = &recordingActivity{events: events}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ag.RunTurn(ctx, "answer"); err == nil {
		t.Fatal("expected cancellation")
	}
	got := events.snapshot()
	if eventCount(got, "context-cancelled") != 1 || eventCount(got, "stop:thinking") != 1 {
		t.Fatalf("cancelled activity lifecycle = %#v", got)
	}
}

func eventIndex(events []string, want string) int {
	for i, event := range events {
		if event == want {
			return i
		}
	}
	return -1
}

func eventIndexContaining(events []string, want string) int {
	for i, event := range events {
		if strings.Contains(event, want) {
			return i
		}
	}
	return -1
}

func eventCount(events []string, want string) int {
	count := 0
	for _, event := range events {
		if event == want {
			count++
		}
	}
	return count
}
