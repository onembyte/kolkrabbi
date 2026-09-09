package tui

import (
	"fmt"
	"sort"
	"strings"
)

// The window over the transcript is ephemeral by design: it is there while
// the agents are, and gone a moment after. What they did is not ephemeral,
// and this is where it is kept — every agent of the last run, with the model
// and effort it ran on, the task it was given and each step it took, ready
// to be read after the window has closed (V40.4).

// rememberRun keeps the agents of the run that is finishing, so the record
// outlives the window. Called as the window closes.
func (c *Controller) rememberRun() {
	if len(c.agentStatuses) == 0 {
		return
	}
	c.lastRun = c.sortedAgents()
	c.lastRunLogs = map[string][]string{}
	for key, lines := range c.agentLogs {
		c.lastRunLogs[key] = append([]string(nil), lines...)
	}
}

// sortedAgents is the run in plan order.
func (c *Controller) sortedAgents() []AgentStatus {
	statuses := make([]AgentStatus, 0, len(c.agentStatuses))
	for _, status := range c.agentStatuses {
		statuses = append(statuses, status)
	}
	sort.SliceStable(statuses, func(i, j int) bool {
		if statuses[i].Index == statuses[j].Index {
			return statuses[i].ID < statuses[j].ID
		}
		return statuses[i].Index < statuses[j].Index
	})
	return statuses
}

// AgentReport is the run in full: the one asked for by "what did those
// agents actually do". It reports the run in front of you while one is
// going, and the last one when none is.
func (c *Controller) AgentReport() string {
	statuses, logs := c.sortedAgents(), c.agentLogs
	if len(statuses) == 0 {
		statuses, logs = c.lastRun, c.lastRunLogs
	}
	if len(statuses) == 0 {
		return "no agents have run in this session yet"
	}
	var out strings.Builder
	running := 0
	for _, status := range statuses {
		if status.State == "working" {
			running++
		}
	}
	fmt.Fprintf(&out, "%d agents", len(statuses))
	if running > 0 {
		fmt.Fprintf(&out, ", %d still working", running)
	}
	out.WriteString("\n")
	for _, status := range statuses {
		fmt.Fprintf(&out, "\n%d %s\n", status.Index, compactAgentField(status.Summary, "task"))
		fmt.Fprintf(&out, "  %s · %s · %s\n",
			compactAgentField(status.Model, "model unknown"),
			compactAgentField(status.Effort, "effort default"),
			compactAgentField(status.State, "working"))
		steps := logs[agentKey(status)]
		if step := compactAgentField(status.Step, ""); step != "" && (len(steps) == 0 || steps[len(steps)-1] != step) {
			steps = append(append([]string(nil), steps...), step)
		}
		for _, line := range steps {
			fmt.Fprintf(&out, "    %s\n", line)
		}
	}
	return out.String()
}
