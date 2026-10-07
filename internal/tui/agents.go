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
	label, model, identity, rest := agentStatusParts(status)
	return truncateAgentLine(label+model+identity+rest, maxAgentStatusRunes)
}

// formatAgentStatusRow fits the row to width. The summary and step yield
// first, then the model; the label, effort and state keep their cells at
// every ordinary width, as they do in the agents' window (agentWindowRow).
func formatAgentStatusRow(status AgentStatus, width int) string {
	if line := formatAgentStatusLine(status); cellWidth(line) <= width {
		return line
	}
	label, _, identity, rest := agentStatusParts(status)
	identity = narrowIdentity(status, identity)
	// One cell for the "…" that ends a row whose summary does not fit.
	model := fitAgentModel(status.Model, width-cellWidth(label+identity)-1)
	return clipLine(label+model+identity+rest, width)
}

// agentStatusParts splits the row into its label and model, the identity
// that must survive (effort and state), and the summary and step.
func agentStatusParts(status AgentStatus) (label, model, identity, rest string) {
	index, total := status.Index, status.Total
	if index < 1 {
		index = 1
	}
	if total < index {
		total = index
	}
	state := compactAgentField(status.State, "working")
	label = fmt.Sprintf("agent [%d/%d] · ", index, total)
	model = compactAgentField(status.Model, "model unknown")
	identity = " · " + compactAgentField(status.Effort, "effort default") + " · " + state
	rest = ": " + compactAgentField(status.Summary, "task")
	if step := compactAgentField(status.Step, ""); step != "" && step != state {
		rest += " — " + step
	}
	return label, model, identity, rest
}

// narrowIdentity drops the "effort default" placeholder from a row that does
// not fit: it only says the effort is unresolved, the least a narrow row has
// to say, and it should not cost the model or the state their cells.
func narrowIdentity(status AgentStatus, identity string) string {
	if compactAgentField(status.Effort, "") != "" {
		return identity
	}
	return " · " + compactAgentField(status.State, "working")
}

// fitAgentModel fits a worker's model into room cells. A model ID that does
// not fit gives way to its short name before it is clipped.
func fitAgentModel(model string, room int) string {
	full := compactAgentField(model, "model unknown")
	if cellWidth(full) <= room {
		return full
	}
	if short := shortModelName(model); short != "" {
		full = short
	}
	return clipLine(full, max(1, room))
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
