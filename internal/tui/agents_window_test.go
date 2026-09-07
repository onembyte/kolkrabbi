package tui

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

func twoAgents() []AgentStatus {
	return []AgentStatus{
		{ID: "a1", Index: 1, Total: 2, Model: "claude-haiku", Effort: "low", Summary: "Add an MIT LICENSE", State: "working", Step: "· Write: LICENSE → ok", Sequence: 3},
		{ID: "a2", Index: 2, Total: 2, Model: "claude-fable", Effort: "medium", Summary: "Write README.md", State: "queued", Step: "queued", Sequence: 1},
	}
}

// While agents run, their rows and the last steps of each sit in a window at
// the top right of the transcript — the way the site's illustration shows
// them — and the full-width rows above the status line are gone. The rows
// underneath keep their width: the window takes columns, never wraps them.
func TestAgentsWindowSitsTopRightWithRowsAndLogs(t *testing.T) {
	m := New(Status{Mode: "agent", Lifecycle: "working"})
	for i := 0; i < 40; i++ {
		m.AppendTranscript(fmt.Sprintf("transcript line %d that is long enough to reach under the window and a bit more\n", i))
	}
	m.SetAgentStatuses(twoAgents())
	m.SetAgentLogs(map[string][]string{"a1": {"preparing a tree of its own", "opening claude-haiku", "· Write: LICENSE → ok"}})

	view := m.View(100, 24)
	rows := strings.Split(view, "\n")
	if !strings.Contains(rows[0], "agents 1/2") {
		t.Fatalf("the top row does not carry the window title:\n%s", view)
	}
	for _, want := range []string{"1 Add an MIT LICENSE · working", "· Write: LICENSE → ok", "2 Write README.md · queued", "opening claude-haiku"} {
		if !strings.Contains(view, want) {
			t.Errorf("the window lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "agent [1/2]") {
		t.Fatalf("the full-width agent row is still drawn beside the window:\n%s", view)
	}
	for i, row := range rows {
		if cellWidth(row) > 100 {
			t.Fatalf("row %d is %d cells wide, over the 100 of the screen: %q", i, cellWidth(row), row)
		}
	}
	// The window is on the right: a transcript row under it still starts
	// with its own text.
	if !strings.HasPrefix(rows[1], "transcript line") {
		t.Fatalf("row 1 lost its transcript text to the window: %q", rows[1])
	}
}

// A screen too narrow for two columns keeps the rows where they were.
func TestAgentsWindowGivesWayOnANarrowScreen(t *testing.T) {
	m := New(Status{Mode: "agent", Lifecycle: "working"})
	m.AppendTranscript("hello\n")
	m.SetAgentStatuses(twoAgents())
	view := m.View(60, 20)
	if !strings.Contains(view, "agent [1/2]") || strings.Contains(view, "agents 1/2") {
		t.Fatalf("narrow screen did not fall back to the rows:\n%s", view)
	}
}

// Every status update is a line of the agent's log, once, newest last, and
// the log is bounded.
func TestAgentLogsCollectEachDistinctStep(t *testing.T) {
	controller := NewController(Status{Mode: "agent", Lifecycle: "working"}, 1024)
	steps := []string{"preparing a tree of its own", "opening claude-haiku", "opening claude-haiku", "· Write: LICENSE → ok", "landing its changes", "completed"}
	for i, step := range steps {
		controller.SetAgentStatus(AgentStatus{ID: "a1", Index: 1, Total: 1, State: "working", Step: step, Sequence: uint64(i + 1)})
	}
	got := controller.Snapshot().AgentLogs["a1"]
	if strings.Join(got, "|") != "opening claude-haiku|· Write: LICENSE → ok|landing its changes|completed" {
		t.Fatalf("log = %q, want the last four distinct steps in order", got)
	}
}

// The window stays a moment after the turn ends, then closes on its own.
func TestAgentsWindowLingersThenClosesAfterTheTurn(t *testing.T) {
	input, keys := io.Pipe()
	defer input.Close()
	var runtime *Runtime
	runtime = NewRuntime(RuntimeOptions{Input: input, Output: io.Discard, Status: Status{Mode: "agent", Lifecycle: "ready"},
		Turn: func(context.Context, string) error {
			runtime.SetAgentStatus(AgentStatus{ID: "a1", Index: 1, Total: 1, Summary: "the task", State: "done", Step: "completed", Sequence: 1})
			return nil
		}})
	clock := newFakeSpinnerClock()
	runtime.spinClock = clock
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = runtime.Run(ctx) }()
	if _, err := keys.Write([]byte("run it\r")); err != nil {
		t.Fatal(err)
	}
	// The spinner asks for its own timers first; the window's is the one
	// with the linger.
	var timer *fakeSpinnerTimer
	deadline := time.After(2 * time.Second)
	for timer == nil {
		select {
		case candidate := <-clock.created:
			if candidate.delay == agentWindowLinger {
				timer = candidate
			}
		case <-deadline:
			t.Fatal("the turn's end never armed the window's close")
		}
	}
	if len(runtime.Snapshot().AgentStatuses) != 1 {
		t.Fatal("the window closed with the turn instead of lingering")
	}
	timer.fire()
	wait := time.Now().Add(time.Second)
	for len(runtime.Snapshot().AgentStatuses) != 0 {
		if time.Now().After(wait) {
			t.Fatal("the window did not close after the linger")
		}
		time.Sleep(5 * time.Millisecond)
	}
	_ = keys.Close()
}
