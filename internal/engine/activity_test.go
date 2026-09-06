package engine

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

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

type persistentRecordingActivity struct {
	*recordingActivity
}

func (persistentRecordingActivity) KeepActivityDuringOutput() bool { return true }

type blockedTokenBackend struct {
	token       chan struct{}
	release     chan struct{}
	returned    chan struct{}
	releaseOnce sync.Once
}

func (b *blockedTokenBackend) releaseNow() {
	b.releaseOnce.Do(func() { close(b.release) })
}

func newBlockedTokenBackend() *blockedTokenBackend {
	return &blockedTokenBackend{
		token: make(chan struct{}), release: make(chan struct{}), returned: make(chan struct{}),
	}
}

func (b *blockedTokenBackend) StreamChat(ctx context.Context, model string, _ []provider.Message,
	_ []provider.Tool, onToken func(string)) (provider.Message, provider.Meta, error) {
	if onToken != nil {
		onToken("hidden token")
	}
	close(b.token)
	select {
	case <-b.release:
		close(b.returned)
		return provider.Message{Role: "assistant", Content: "done"}, provider.Meta{Model: model}, nil
	case <-ctx.Done():
		return provider.Message{}, provider.Meta{Model: model}, ctx.Err()
	}
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

func TestPersistentActivityStaysVisibleThroughVisibleTokens(t *testing.T) {
	srv := enginetest.New(enginetest.Step{Text: "visible answer"})
	defer srv.Close()

	ag, _, _, _ := newTestAgentInternal(t, srv, ModeCode)
	events := &activityEvents{}
	ag.Out = events
	ag.Activity = persistentRecordingActivity{recordingActivity: &recordingActivity{events: events}}
	if err := ag.RunTurn(context.Background(), "answer"); err != nil {
		t.Fatal(err)
	}
	got := events.snapshot()
	start := eventIndex(got, "start:thinking")
	stop := eventIndex(got, "stop:thinking")
	content := eventIndexContaining(got, "visible")
	if start < 0 || content <= start || stop <= content {
		t.Fatalf("persistent activity/token order = %#v", got)
	}
	if eventCount(got, "start:thinking") != 1 || eventCount(got, "stop:thinking") != 1 {
		t.Fatalf("persistent activity lifecycle was not exactly once: %#v", got)
	}
}

func TestBufferedSubagentTokensKeepActivityUntilEachBackendReturns(t *testing.T) {
	first, second := newBlockedTokenBackend(), newBlockedTokenBackend()
	t.Cleanup(first.releaseNow)
	t.Cleanup(second.releaseNow)
	srv := enginetest.New()
	defer srv.Close()
	agent, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	agent.MaxConcurrentTasks = 2
	events := &activityEvents{}
	agent.Activity = &recordingActivity{events: events}
	backends := map[string]ChatBackend{"model/first": first, "model/second": second}
	agent.Root = t.TempDir()
	agent.SubagentBackend = func(_ context.Context, model, _, _ string, _ SubagentCapabilities) (ChatBackend, error) {
		return backends[model], nil
	}

	runDone := make(chan error, 1)
	go func() {
		_, err := agent.runTasks(context.Background(), "two checks", []Task{
			{Title: "first", Kind: KindResearch, Model: "model/first"},
			{Title: "second", Kind: KindResearch, Model: "model/second"},
		})
		runDone <- err
	}()

	waitSignal(t, first.token, "first hidden token")
	waitSignal(t, second.token, "second hidden token")
	prematureStops := eventCount(events.snapshot(), "stop:working")

	first.releaseNow()
	waitSignal(t, first.returned, "first backend return")
	waitForEventCount(t, events, "stop:working", 1)
	activeAfterFirst := eventCount(events.snapshot(), "start:working") -
		eventCount(events.snapshot(), "stop:working")

	second.releaseNow()
	waitSignal(t, second.returned, "second backend return")
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("runTasks: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subagent run did not finish")
	}

	if prematureStops != 0 {
		t.Errorf("%d activities stopped on hidden buffered tokens, want none", prematureStops)
	}
	if activeAfterFirst != 1 {
		t.Errorf("active activities after first backend = %d, want the second spinner still alive", activeAfterFirst)
	}
	if starts, stops := eventCount(events.snapshot(), "start:working"), eventCount(events.snapshot(), "stop:working"); starts != 2 || stops != 2 {
		t.Errorf("activity lifecycle = %d starts/%d stops, want 2/2", starts, stops)
	}
}

func waitSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}

func waitForEventCount(t *testing.T, events *activityEvents, want string, count int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for eventCount(events.snapshot(), want) < count && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := eventCount(events.snapshot(), want); got < count {
		t.Fatalf("%s count = %d, want at least %d", want, got, count)
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
		// runLoop prints its trailing newline only after streamChat has stopped;
		// a 503 is a capacity limit that waiting lifts, so the turn then pauses
		// and says so (V35.2a) -- after the newline, never before the stop.
		newline := eventIndex(got, "write:\n")
		if eventCount(got, "start:thinking") != 1 || eventCount(got, "stop:thinking") != 1 || newline < 0 {
			t.Fatalf("error lifecycle = %#v", got)
		}
		if stop := eventIndex(got, "stop:thinking"); stop >= newline {
			t.Fatalf("error returned before activity stopped: %#v", got)
		}
		if !strings.Contains(got[len(got)-1], "paused") {
			t.Fatalf("a capacity limit did not end in a pause notice: %#v", got)
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
