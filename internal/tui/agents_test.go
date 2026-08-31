package tui

import (
	"strings"
	"sync"
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

func TestAgentStatusIsACompactSingleLinePreview(t *testing.T) {
	got := formatAgentStatusLine(AgentStatus{
		Index: 1, Total: 3, Model: "gpt-5.6-luna", Effort: "medium",
		Summary: "Review the architecture\nthen inspect every runtime path and report the important findings",
		State:   "working",
	})
	if !strings.HasPrefix(got, "agent [1/3] - gpt-5.6-luna - medium - ") {
		t.Fatalf("agent row = %q, want the compact agent prefix", got)
	}
	if strings.ContainsAny(got, "\r\n") {
		t.Fatalf("agent preview escaped its one-line boundary: %q", got)
	}
	if len([]rune(got)) > 120 {
		t.Fatalf("agent preview is %d runes, want at most 120: %q", len([]rune(got)), got)
	}
	if !strings.Contains(got, "working") {
		t.Fatalf("agent state is missing from the preview: %q", got)
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

func TestAgentLifecycleRowsRemainThroughSynthesisThenClear(t *testing.T) {
	c := NewController(Status{Mode: "agent", Effort: "high", Lifecycle: "working"}, 4096)
	first := AgentStatus{
		ID: "task-1", Index: 1, Total: 2, Model: "gpt-5.6-luna", Effort: "low",
		Summary: "Inspect the repository", State: "working",
	}
	second := AgentStatus{
		ID: "task-2", Index: 2, Total: 2, Model: "gpt-5.6-sol", Effort: "max",
		Summary: "Reason about the concurrency boundary", State: "working",
	}
	c.SetAgentStatus(second)
	c.SetAgentStatus(first)

	got := c.Snapshot()
	if got.Status.Agents != 2 || len(got.AgentStatuses) != 2 {
		t.Fatalf("running status = %+v, agents = %+v", got.Status, got.AgentStatuses)
	}
	if got.AgentStatuses[0].ID != "task-1" || got.AgentStatuses[1].ID != "task-2" {
		t.Fatalf("agent rows are not stable plan order: %+v", got.AgentStatuses)
	}
	view := c.View(160, 20)
	for _, want := range []string{
		"agent [1/2] - gpt-5.6-luna - low - working: Inspect the repository",
		"agent [2/2] - gpt-5.6-sol - max - working: Reason about the concurrency boundary",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view is missing %q:\n%s", want, view)
		}
	}

	first.State = "done"
	c.SetAgentStatus(first)
	if got := c.Snapshot(); got.Status.Agents != 1 || got.AgentStatuses[0].State != "done" {
		t.Fatalf("finished agent did not remain visible through synthesis: %+v", got)
	}
	second.State = "failed"
	c.SetAgentStatus(second)
	if got := c.Snapshot(); got.Status.Agents != 0 || got.AgentStatuses[1].State != "failed" {
		t.Fatalf("failed agent did not leave the running count and remain visible: %+v", got)
	}

	c.FinishTurn("ready")
	if got := c.Snapshot(); got.Status.Agents != 0 || len(got.AgentStatuses) != 0 {
		t.Fatalf("finished turn retained stale agent rows: %+v", got)
	}
}

func TestRuntimeAgentStatusUpdatesAreRaceSafe(t *testing.T) {
	runtime := NewRuntime(RuntimeOptions{})
	var wg sync.WaitGroup
	for i := 1; i <= 32; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			runtime.SetAgentStatus(AgentStatus{
				ID: string(rune(index)), Index: index, Total: 32,
				Model: "gpt-5.6-luna", Effort: "medium", Summary: "task", State: "working",
			})
		}(i)
	}
	wg.Wait()

	got := runtime.Snapshot()
	if got.Status.Agents != 32 || len(got.AgentStatuses) != 32 {
		t.Fatalf("concurrent updates lost agents: count %d rows %d", got.Status.Agents, len(got.AgentStatuses))
	}
}
