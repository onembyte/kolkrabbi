package tui

import (
	"bytes"
	"fmt"
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

// An active turn must never leave the footer saying "state working" after the
// layout has discarded the only animated indicator. Agent rows are useful
// detail, but the spinner is the liveness proof the whole run is still alive.
func TestWorkingStateKeepsItsSpinnerWhenAgentRowsUseTheHeight(t *testing.T) {
	m := New(Status{
		Mode: "agent", Effort: "medium", Approval: "full-auto", Lifecycle: "working",
	})
	m.SetActivity(activityLine(0, "working"))
	for index := 1; index <= 3; index++ {
		m.SetAgentStatuses(append(m.Snapshot().AgentStatuses, AgentStatus{
			ID: fmt.Sprintf("task-%d", index), Index: index, Total: 3,
			Model: "gpt-5.6-luna", Effort: "medium", Summary: "task", State: "working",
		}))
	}

	view := m.View(80, 7)
	if !strings.Contains(view, "working") {
		t.Fatalf("working state disappeared from the constrained frame:\n%s", view)
	}
	if !strings.Contains(view, activityLine(0, "working")) {
		t.Fatalf("working state has no visible spinner in the constrained frame:\n%s", view)
	}
}

func TestWorkingStateAndSpinnerStayConsistentAcrossUsableFrames(t *testing.T) {
	for _, width := range []int{32, 48, 80, 120, 160} {
		for height := 4; height <= 14; height++ {
			t.Run(fmt.Sprintf("%dx%d", width, height), func(t *testing.T) {
				m := New(Status{
					Mode: "agent", Effort: "medium", Approval: "full-auto", Lifecycle: "working",
				})
				m.SetActivity(activityLine(0, "working"))
				statuses := make([]AgentStatus, 0, 6)
				for index := 1; index <= 6; index++ {
					statuses = append(statuses, AgentStatus{
						ID: fmt.Sprintf("task-%d", index), Index: index, Total: 6,
						Model: "gpt-5.6-luna", Effort: "medium", Summary: "task", State: "working",
					})
				}
				m.SetAgentStatuses(statuses)

				view := m.View(width, height)
				if strings.Contains(view, "state working") && !strings.Contains(view, "working…") {
					t.Fatalf("working state rendered without its spinner at %dx%d:\n%s", width, height, view)
				}
			})
		}
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
		State:   "working", Step: "provider tool Read file started",
	})
	if !strings.HasPrefix(got, "agent [1/3] · gpt-5.6-luna · medium · working: ") {
		t.Fatalf("agent row = %q, want the compact agent prefix", got)
	}
	if strings.ContainsAny(got, "\r\n") {
		t.Fatalf("agent preview escaped its one-line boundary: %q", got)
	}
	if len([]rune(got)) > maxAgentStatusRunes {
		t.Fatalf("agent preview is %d runes, want at most %d: %q", len([]rune(got)), maxAgentStatusRunes, got)
	}
	if !strings.Contains(got, "working") || !strings.Contains(got, "provider tool") {
		t.Fatalf("agent state or latest step is missing from the preview: %q", got)
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
		Summary: "Inspect the repository", State: "working", Step: "model is responding", Sequence: 1,
	}
	second := AgentStatus{
		ID: "task-2", Index: 2, Total: 2, Model: "gpt-5.6-sol", Effort: "max",
		Summary: "Reason about the concurrency boundary", State: "working", Step: "opening gpt-5.6-sol", Sequence: 1,
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
		"agent [1/2] · gpt-5.6-luna · low · working: Inspect the repository — model is responding",
		"agent [2/2] · gpt-5.6-sol · max · working: Reason about the concurrency boundary — opening gpt-5.6-sol",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view is missing %q:\n%s", want, view)
		}
	}

	first.State, first.Step, first.Sequence = "done", "completed", 2
	c.SetAgentStatus(first)
	if got := c.Snapshot(); got.Status.Agents != 1 || got.AgentStatuses[0].State != "done" {
		t.Fatalf("finished agent did not remain visible through synthesis: %+v", got)
	}
	second.State, second.Step, second.Sequence = "failed", "provider refused", 2
	c.SetAgentStatus(second)
	if got := c.Snapshot(); got.Status.Agents != 0 || got.AgentStatuses[1].State != "failed" {
		t.Fatalf("failed agent did not leave the running count and remain visible: %+v", got)
	}

	c.FinishTurn("ready")
	if got := c.Snapshot(); got.Status.Agents != 0 || len(got.AgentStatuses) != 0 {
		t.Fatalf("finished turn retained stale agent rows: %+v", got)
	}
}

func TestAgentStatusIgnoresAnOlderPerTaskUpdate(t *testing.T) {
	c := NewController(Status{Mode: "agent", Lifecycle: "working"}, 4096)
	c.SetAgentStatus(AgentStatus{
		ID: "task-1", Index: 1, Total: 1, Model: "gpt-5.6-luna", Effort: "medium",
		Summary: "Review runtime", State: "working", Step: "running tests", Sequence: 4,
	})
	c.SetAgentStatus(AgentStatus{
		ID: "task-1", Index: 1, Total: 1, Model: "gpt-5.6-luna", Effort: "medium",
		Summary: "Review runtime", State: "waiting", Step: "waiting for task 0", Sequence: 3,
	})
	got := c.Snapshot().AgentStatuses
	if len(got) != 1 || got[0].State != "working" || got[0].Step != "running tests" || got[0].Sequence != 4 {
		t.Fatalf("stale task update replaced the latest row: %+v", got)
	}
}

func TestAgentStatusRowsUseSemanticStateStyles(t *testing.T) {
	for _, test := range []struct {
		state string
		want  rowStyle
	}{
		{state: "queued", want: stylePurpleMuted},
		{state: "waiting", want: styleWarn},
		{state: "working", want: stylePurple},
		{state: "done", want: styleAdd},
		{state: "failed", want: styleDel},
		{state: "blocked", want: styleWarn},
	} {
		t.Run(test.state, func(t *testing.T) {
			m := New(Status{Mode: "agent"})
			m.SetAgentStatuses([]AgentStatus{{
				ID: "task-1", Index: 1, Total: 1, Model: "gpt-5.6-luna", Effort: "medium",
				Summary: "Review", State: test.state, Step: "current step",
			}})
			for _, row := range m.viewRows(200, 20, 0) {
				if strings.HasPrefix(row.text, "agent [1/1]") {
					if row.style != test.want {
						t.Fatalf("state %q style = %v, want %v", test.state, row.style, test.want)
					}
					return
				}
			}
			t.Fatalf("agent row missing for state %q", test.state)
		})
	}
}

func TestAgentStatusRowsRemainMeaningfulWithoutColour(t *testing.T) {
	SetPalette("none")
	t.Cleanup(func() { SetPalette("256") })
	c := NewController(Status{Mode: "agent", Lifecycle: "working"}, 4096)
	c.SetAgentStatus(AgentStatus{
		ID: "task-1", Index: 1, Total: 1, Model: "gpt-5.6-luna", Effort: "medium",
		Summary: "Review runtime", State: "waiting", Step: "waiting for task 1",
	})
	view := c.RenderView(160, 20)
	if strings.Contains(view, "\x1b[") {
		t.Fatalf("NO_COLOR agent row contains ANSI: %q", view)
	}
	if !strings.Contains(view, "waiting: Review runtime — waiting for task 1") {
		t.Fatalf("NO_COLOR agent row lost its state: %q", view)
	}
}

func TestAgentStatusRowsClipSafelyInANarrowFrame(t *testing.T) {
	m := New(Status{Mode: "agent"})
	m.SetAgentStatuses([]AgentStatus{{
		ID: "task-1", Index: 1, Total: 1, Model: "gpt-5.6-luna", Effort: "medium",
		Summary: strings.Repeat("summary ", 20), State: "working", Step: strings.Repeat("step ", 20),
	}})
	const width = 42
	for _, row := range m.viewRows(width, 20, 0) {
		if strings.HasPrefix(row.text, "agent [1/1]") {
			if cellWidth(row.text) > width || !strings.HasSuffix(row.text, "…") || strings.ContainsAny(row.text, "\r\n") {
				t.Fatalf("narrow agent row is unsafe: %q", row.text)
			}
			return
		}
	}
	t.Fatal("narrow frame lost its agent row")
}

// Resize must redraw the same ordered task rows, not reconstruct them from
// stale renderer output. Drive the Runtime path (where concurrent engine
// callbacks and terminal reflow meet) with deliberately hostile fields.
func TestRuntimeAgentRowsKeepOrderMeaningAndSafetyAcrossResize(t *testing.T) {
	SetPalette("none")
	t.Cleanup(func() { SetPalette("256") })

	var output bytes.Buffer
	width := 120
	runtime := NewRuntime(RuntimeOptions{
		Output: &output,
		Width:  func() int { return width },
		Height: func() int { return 18 },
		Status: Status{Mode: "agent", Lifecycle: "working"},
	})
	// Send index 2 first, as concurrent child callbacks naturally do.
	runtime.SetAgentStatus(AgentStatus{
		ID: "task-2", Index: 2, Total: 2, Model: "luna", Effort: "low",
		Summary: "second task\x1b[2J\n" + strings.Repeat("detail ", 24),
		State:   "waiting", Step: "waiting for task 1\x1b]8;;https://invalid\a",
		Sequence: 1,
	})
	runtime.SetAgentStatus(AgentStatus{
		ID: "task-1", Index: 1, Total: 2, Model: "luna", Effort: "low",
		Summary: "first task\r" + strings.Repeat("detail ", 24),
		State:   "working", Step: "provider tool read started\x1b[31m",
		Sequence: 1,
	})

	assertRuntimeAgentResizeFrame(t, runtime, 120)
	width = 48
	runtime.Resize()
	assertRuntimeAgentResizeFrame(t, runtime, 48)
	width = 96
	runtime.Resize()
	assertRuntimeAgentResizeFrame(t, runtime, 96)

	got := runtime.Snapshot().AgentStatuses
	if len(got) != 2 || got[0].ID != "task-1" || got[1].ID != "task-2" ||
		got[0].State != "working" || got[1].State != "waiting" {
		t.Fatalf("resize changed task order or state meaning: %+v", got)
	}
}

func assertRuntimeAgentResizeFrame(t *testing.T, runtime *Runtime, width int) {
	t.Helper()
	// The pacing timer deliberately coalesces ordinary updates; close it here
	// so each size assertion reads the exact frame the runtime would paint at
	// the resize boundary without a timing-dependent sleep.
	runtime.mu.Lock()
	runtime.flushFrameLocked()
	frame := runtime.renderer.lastView
	runtime.mu.Unlock()

	if strings.Contains(frame, "\x1b") || strings.ContainsAny(frame, "\r\x07") {
		t.Fatalf("hostile task text reached resized frame: %q", frame)
	}
	first, second := strings.Index(frame, "agent [1/2] · luna · low · working:"),
		strings.Index(frame, "agent [2/2] · luna · low · waiting:")
	if first < 0 || second < 0 || first >= second {
		t.Fatalf("resized frame lost ordered state rows at width %d:\n%s", width, frame)
	}
	for _, line := range strings.Split(frame, "\n") {
		if cellWidth(line) > width {
			t.Fatalf("resized frame exceeds %d cells: %q", width, line)
		}
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
