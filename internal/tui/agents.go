package tui

import (
	"fmt"
	"strings"
)

// AgentStatus is the small, presentation-owned view of one orchestrated task.
// ID is only the opaque correlation key used to replace this row; provider
// handles, conversation ids, token counts and timings never reach the view.
type AgentStatus struct {
	ID      string
	Index   int
	Total   int
	Model   string
	Effort  string
	Summary string
	State   string
	Phase   string
	Step    string
	// Sequence is the engine's monotonic per-task replacement token. It is
	// not rendered; the controller uses it to reject an older callback that
	// reaches the TUI after a newer observed boundary.
	Sequence uint64
}

const maxAgentStatusRunes = 160

// formatAgentStatusLine renders the stable one-row shape shown while an
// orchestrated task is in flight. Planner text is untrusted terminal content,
// so every field is sanitised and whitespace-folded before it reaches the row.
func formatAgentStatusLine(status AgentStatus) string {
	index, total := status.Index, status.Total
	if index < 1 {
		index = 1
	}
	if total < index {
		total = index
	}
	model := compactAgentField(status.Model, "model unknown")
	effort := compactAgentField(status.Effort, "effort default")
	state := compactAgentField(status.State, "working")
	summary := compactAgentField(status.Summary, "task")
	step := compactAgentField(status.Step, "")

	line := fmt.Sprintf("agent [%d/%d] · %s · %s · %s: %s",
		index, total, model, effort, state, summary)
	if step != "" && step != state {
		line += " — " + step
	}
	return truncateAgentLine(line, maxAgentStatusRunes)
}

func compactAgentField(value, fallback string) string {
	value = strings.Join(strings.Fields(sanitizeTerminalLine(value)), " ")
	if value == "" {
		return fallback
	}
	return value
}

func truncateAgentLine(line string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(line)
	if len(runes) <= limit {
		return line
	}
	if limit == 1 {
		return "…"
	}
	return string(runes[:limit-1]) + "…"
}
