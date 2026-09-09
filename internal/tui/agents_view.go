package tui

import (
	"fmt"
	"strings"
)

// The full view of a run (V40.5). The window over the transcript is a
// glance; this is the whole thing — every agent, the goal it was given, the
// model and effort it runs on, and the steps of the one being read. It opens
// on the left arrow from an empty composer, where the arrow has nothing else
// to do, and gives the composer back on Escape.

// hasRunToShow reports whether there is a run worth opening: the one going
// now, or the last one to have gone.
func (c *Controller) hasRunToShow() bool {
	return len(c.agentStatuses) > 0 || len(c.lastRun) > 0
}

// runToShow is the run the view reads, in plan order.
func (c *Controller) runToShow() ([]AgentStatus, map[string][]string) {
	if len(c.agentStatuses) > 0 {
		return c.sortedAgents(), c.agentLogs
	}
	return c.lastRun, c.lastRunLogs
}

// openAgentsView shows the run in full.
func (c *Controller) openAgentsView() Effect {
	c.agentsView = true
	c.agentsIndex = 0
	c.screen.HideAgentWindow(true)
	c.clearSuggestions()
	return Effect{}
}

func (c *Controller) closeAgentsView() Effect {
	c.agentsView = false
	c.screen.HideAgentWindow(false)
	return Effect{}
}

// handleAgentsViewKey walks the run. The keys are the ones the other lists
// use, so nothing here has to be learned twice.
func (c *Controller) handleAgentsViewKey(key Key) Effect {
	agents, _ := c.runToShow()
	count := len(agents)
	if count == 0 {
		return c.closeAgentsView()
	}
	switch key.Kind {
	case KeyEscape, KeyLeft, KeyInterrupt, KeyEOF, KeyEnter:
		return c.closeAgentsView()
	case KeyDown, KeyTab:
		c.agentsIndex = (c.agentsIndex + 1) % count
	case KeyUp, KeyShiftTab:
		if c.agentsIndex <= 0 {
			c.agentsIndex = count - 1
		} else {
			c.agentsIndex--
		}
	case KeyPageDown:
		c.agentsIndex = min(count-1, c.agentsIndex+c.windowSize())
	case KeyPageUp:
		c.agentsIndex = max(0, c.agentsIndex-c.windowSize())
	case KeyHome:
		c.agentsIndex = 0
	case KeyEnd:
		c.agentsIndex = count - 1
	}
	return Effect{}
}

// agentsViewLines draws the run: one row per agent, and under the one being
// read, everything known about it.
func (c *Controller) agentsViewLines(width int) []string {
	agents, logs := c.runToShow()
	running := 0
	for _, status := range agents {
		if status.State == "working" {
			running++
		}
	}
	title := fmt.Sprintf("agents — %d in this run", len(agents))
	if running > 0 {
		title = fmt.Sprintf("agents — %d of %d still working", running, len(agents))
	}
	lines := []string{
		horizontalRule(title, width),
		clipLine("↑/↓ to walk the run · Esc or ← to go back", width),
		"",
	}
	for index, status := range agents {
		marker := "  "
		if index == c.agentsIndex {
			marker = "> "
		}
		lines = append(lines, clipLine(fmt.Sprintf("%s%d %s", marker, status.Index,
			compactAgentField(status.Summary, "task")), width))
		lines = append(lines, clipLine(fmt.Sprintf("    %s · %s · %s",
			compactAgentField(status.Model, "model unknown"),
			compactAgentField(status.Effort, "effort default"),
			compactAgentField(status.State, "working")), width))
		if index != c.agentsIndex {
			continue
		}
		steps := logs[agentKey(status)]
		if step := compactAgentField(status.Step, ""); step != "" && (len(steps) == 0 || steps[len(steps)-1] != step) {
			steps = append(append([]string(nil), steps...), step)
		}
		if len(steps) == 0 {
			lines = append(lines, clipLine("      nothing recorded yet", width))
			continue
		}
		for _, step := range steps {
			lines = append(lines, clipLine("      "+sanitizeTerminalLine(step), width))
		}
	}
	return lines
}

// agentsViewOpensOn reports whether this key should open the view: the left
// arrow, from an empty composer, with a run to show. With anything typed the
// arrow is still the arrow.
func (c *Controller) agentsViewOpensOn(key Key) bool {
	return key.Kind == KeyLeft && strings.TrimSpace(c.editor.Draft()) == "" && c.hasRunToShow()
}
