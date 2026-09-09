package tui

import (
	"strings"
	"testing"
)

func agentsRunning(c *Controller) {
	for _, status := range sixAgents() {
		c.SetAgentStatus(status)
	}
	c.SetAgentStatus(AgentStatus{ID: "a2", Index: 2, Total: 6, Model: "claude-fable", Effort: "medium",
		Summary: "task number 2", State: "working", Step: "· Write: README.md → ok", Sequence: 2})
}

// The left arrow opens the whole run: every agent, its goal, effort, model
// and state, with the one you are on expanded into its steps. Up and down
// walk the list; Esc gives the composer back.
func TestTheLeftArrowOpensTheWholeRun(t *testing.T) {
	c := NewController(Status{Mode: "agent", Lifecycle: "working"}, 4096)
	agentsRunning(c)

	if effect := c.HandleKey(Key{Kind: KeyLeft}); effect.Submit != "" {
		t.Fatalf("opening the view submitted: %#v", effect)
	}
	view := c.View(100, 24)
	for i := 1; i <= 6; i++ {
		if !strings.Contains(view, "task number "+string(rune('0'+i))) {
			t.Fatalf("the view lacks agent %d:\n%s", i, view)
		}
	}
	for _, want := range []string{"claude-haiku", "low", "working"} {
		if !strings.Contains(view, want) {
			t.Errorf("the view lacks %q:\n%s", want, view)
		}
	}

	// The second agent's steps show once it is the one selected.
	c.HandleKey(Key{Kind: KeyDown})
	if got := c.View(100, 24); !strings.Contains(got, "· Write: README.md → ok") {
		t.Fatalf("the selected agent's steps are not shown:\n%s", got)
	}

	// Escape gives the composer back; the ordinary window over the
	// transcript is still there, which is why the view's own chrome is what
	// tells them apart.
	c.HandleKey(Key{Kind: KeyEscape})
	if got := c.View(100, 24); strings.Contains(got, "to walk the run") {
		t.Fatalf("escape did not close the view:\n%s", got)
	}
}

// The left arrow is still the left arrow: with something typed it moves the
// caret, and with no run to show it does nothing at all.
func TestTheLeftArrowStillMovesTheCaret(t *testing.T) {
	c := NewController(Status{Mode: "agent", Lifecycle: "working"}, 4096)
	agentsRunning(c)
	c.HandleKey(Key{Kind: KeyText, Text: "hello"})
	c.HandleKey(Key{Kind: KeyLeft})
	if got := c.View(100, 24); strings.Contains(got, "to walk the run") {
		t.Fatalf("the left arrow opened the view over a draft:\n%s", got)
	}
	if got := c.Cursor(); got != 4 {
		t.Fatalf("the caret is at %d, want 4", got)
	}

	empty := NewController(Status{Mode: "code", Lifecycle: "ready"}, 4096)
	empty.HandleKey(Key{Kind: KeyLeft})
	if got := empty.View(100, 24); strings.Contains(got, "to walk the run") {
		t.Fatalf("the view opened with no run to show:\n%s", got)
	}
}

// The view reads the last run once the window has gone, which is the point
// of keeping the record.
func TestTheViewReadsTheLastRunAfterItEnds(t *testing.T) {
	c := NewController(Status{Mode: "agent", Lifecycle: "working"}, 4096)
	agentsRunning(c)
	c.FinishTurn("ready")
	c.CloseAgentWindow()
	c.HandleKey(Key{Kind: KeyLeft})
	got := c.View(100, 24)
	if !strings.Contains(got, "to walk the run") || !strings.Contains(got, "task number 6") {
		t.Fatalf("the view does not read the finished run:\n%s", got)
	}
}
