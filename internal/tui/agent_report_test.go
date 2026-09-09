package tui

import (
	"strings"
	"testing"
)

// The window is ephemeral, the record is not. When the run is over and the
// window has gone, what each agent did is still there to read: every agent,
// its model and effort, the task it was given, and its steps in order.
func TestTheRunsRecordOutlivesItsWindow(t *testing.T) {
	c := NewController(Status{Mode: "agent", Lifecycle: "working"}, 4096)
	if got := c.AgentReport(); !strings.Contains(got, "no agents have run") {
		t.Fatalf("before any run the report says %q", got)
	}
	for _, status := range sixAgents() {
		c.SetAgentStatus(status)
	}
	c.SetAgentStatus(AgentStatus{ID: "a2", Index: 2, Total: 6, Model: "claude-fable", Effort: "medium",
		Summary: "task number 2", State: "working", Step: "· Write: README.md → ok", Sequence: 2})
	c.SetAgentStatus(AgentStatus{ID: "a2", Index: 2, Total: 6, Model: "claude-fable", Effort: "medium",
		Summary: "task number 2", State: "done", Step: "completed", Sequence: 3})

	c.FinishTurn("ready")
	c.CloseAgentWindow()
	if len(c.Snapshot().AgentStatuses) != 0 {
		t.Fatal("the window did not close")
	}

	report := c.AgentReport()
	for _, want := range []string{
		"6 agents", "1 task number 1", "6 task number 6",
		"claude-fable", "medium", "· Write: README.md → ok", "completed",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("the record lacks %q:\n%s", want, report)
		}
	}
	// Its steps stay in the order they happened.
	if first, second := strings.Index(report, "· Write: README.md → ok"), strings.Index(report, "completed"); first < 0 || second < first {
		t.Fatalf("the steps are out of order:\n%s", report)
	}
}

// A new run replaces the record: the question "what are the agents doing" is
// about the run in front of you.
func TestANewRunReplacesTheRecord(t *testing.T) {
	c := NewController(Status{Mode: "agent", Lifecycle: "working"}, 4096)
	c.SetAgentStatus(AgentStatus{ID: "old", Index: 1, Total: 1, Summary: "the old task", State: "done", Step: "completed"})
	c.FinishTurn("ready")
	c.CloseAgentWindow()
	if !strings.Contains(c.AgentReport(), "the old task") {
		t.Fatal("the first run left no record")
	}
	c.SetAgentStatus(AgentStatus{ID: "new", Index: 1, Total: 1, Summary: "the new task", State: "working", Step: "opening"})
	report := c.AgentReport()
	if strings.Contains(report, "the old task") || !strings.Contains(report, "the new task") {
		t.Fatalf("a new run did not replace the record:\n%s", report)
	}
}
