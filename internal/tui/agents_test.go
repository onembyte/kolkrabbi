package tui

import (
	"strings"
	"testing"
)

func statusWith(agents int) []string {
	return formatStatus(Status{
		Mode: "agent", Effort: "medium", Model: "some/model",
		Folder: "~/p", Lifecycle: "working", Approval: "ask", Agents: agents,
	})
}

// The question a long orchestrated run raises is "is this still working, and
// how wide did it go". The answer belongs where the person is already looking.
func TestRunningAgentsAppearInTheStatus(t *testing.T) {
	out := strings.Join(statusWith(3), "\n")
	if !strings.Contains(out, "agents 3") {
		t.Errorf("three running subagents are not shown:\n%s", out)
	}
	// Beside the mode, not on the numbers row: this is what the run is doing,
	// which is that group's subject.
	if !strings.Contains(statusWith(3)[0], "agents 3") {
		t.Errorf("the count is not on the row that carries mode and state:\n%s", out)
	}
}

// Zero is the normal state of every session that never opens a subagent, and a
// permanent "agents 0" is the sort of always-there number people stop reading.
func TestNoAgentsShowsNothing(t *testing.T) {
	if out := strings.Join(statusWith(0), "\n"); strings.Contains(out, "agents") {
		t.Errorf("a session with no subagents still mentions them:\n%s", out)
	}
}

// It is a count, not a progress bar (item 29's refusal): no percentage, no
// elapsed time, no per-agent detail.
func TestTheCountIsOnlyACount(t *testing.T) {
	out := strings.Join(statusWith(2), "\n")
	for _, forbidden := range []string{"%", "elapsed", "eta"} {
		if strings.Contains(strings.ToLower(out), forbidden) {
			t.Errorf("the status grew a progress indicator (%q):\n%s", forbidden, out)
		}
	}
}

// The controller updates one field and re-renders, the way SetApproval does.
func TestSetAgentsUpdatesTheLiveStatus(t *testing.T) {
	c := NewController(Status{Mode: "agent", Effort: "medium", Lifecycle: "working"}, 4096)

	c.SetAgents(4)
	if got := c.Snapshot().Status.Agents; got != 4 {
		t.Errorf("the screen was told %d agents, want 4", got)
	}
	c.SetAgents(0)
	if got := c.Snapshot().Status.Agents; got != 0 {
		t.Errorf("the count did not come back down: %d", got)
	}
}
