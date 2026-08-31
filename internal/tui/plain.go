package tui

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/onembyte/kolkrabbi/protocol"
)

const (
	plainColorReset = "\033[0m"
	plainColorDim   = "\033[2m"
	plainColorCyan  = "\033[36m"
)

// PlainRenderer formats protocol events as human-readable terminal text.
type PlainRenderer struct {
	out   io.Writer
	color bool
}

// NewPlainRenderer constructs a PlainRenderer writing to out.
func NewPlainRenderer(out io.Writer) *PlainRenderer {
	return &PlainRenderer{out: out, color: true}
}

// RenderEvent decodes and formats one protocol envelope.
func (p *PlainRenderer) RenderEvent(env protocol.Envelope) error {
	switch env.Type {
	case protocol.EventMessageDelta:
		var d protocol.MessageDeltaData
		if err := json.Unmarshal(env.Data, &d); err != nil {
			return err
		}
		_, err := io.WriteString(p.out, d.Text)
		return err

	case protocol.EventReasoningDelta:
		var d protocol.ReasoningDeltaData
		if err := json.Unmarshal(env.Data, &d); err != nil {
			return err
		}
		if p.color {
			_, err := fmt.Fprintf(p.out, "%s%s%s", plainColorDim, d.Text, plainColorReset)
			return err
		}
		_, err := io.WriteString(p.out, d.Text)
		return err

	case protocol.EventToolRequested:
		var d protocol.ToolRequestedData
		if err := json.Unmarshal(env.Data, &d); err != nil {
			return err
		}
		name := d.Name
		if name == "" {
			name = "tool"
		}
		if p.color {
			_, err := fmt.Fprintf(p.out, "%s  → Using tool — %s%s\n", plainColorDim, name, plainColorReset)
			return err
		}
		_, err := fmt.Fprintf(p.out, "  → Using tool — %s\n", name)
		return err

	case protocol.EventToolOutput:
		var d protocol.ToolOutputData
		if err := json.Unmarshal(env.Data, &d); err != nil {
			return err
		}
		if d.Output != "" {
			_, err := fmt.Fprintf(p.out, "%s\n", d.Output)
			return err
		}

	case protocol.EventWorkUpdated:
		var d protocol.WorkUpdatedData
		if err := json.Unmarshal(env.Data, &d); err != nil {
			return err
		}
		line := formatWorkUpdatedLine(d)
		if p.color {
			_, err := fmt.Fprintf(p.out, "%s%s%s\n", plainColorDim, line, plainColorReset)
			return err
		}
		_, err := fmt.Fprintln(p.out, line)
		return err

	case protocol.EventUsageReported:
		var d protocol.UsageReportedData
		if err := json.Unmarshal(env.Data, &d); err != nil {
			return err
		}
		var metaParts []string
		if d.Model != "" {
			metaParts = append(metaParts, d.Model)
		}
		toks := int64(0)
		if d.InputTokens != nil {
			toks += *d.InputTokens
		}
		if d.OutputTokens != nil {
			toks += *d.OutputTokens
		}
		if toks > 0 {
			metaParts = append(metaParts, fmt.Sprintf("%d tok", toks))
		}
		if d.CostUSD != nil && *d.CostUSD > 0 {
			metaParts = append(metaParts, fmt.Sprintf("$%.4f", *d.CostUSD))
		}
		if d.TTFTMilliseconds != nil {
			metaParts = append(metaParts, fmt.Sprintf("%dms", *d.TTFTMilliseconds))
		}
		summary := strings.Join(metaParts, " · ")
		if summary != "" {
			if p.color {
				_, err := fmt.Fprintf(p.out, "%s  [%s]%s\n", plainColorDim, summary, plainColorReset)
				return err
			}
			_, err := fmt.Fprintf(p.out, "  [%s]\n", summary)
			return err
		}

	case protocol.EventMessageCompleted:
		_, err := io.WriteString(p.out, "\n")
		return err
	}
	return nil
}

// formatWorkUpdatedLine keeps durable milestones chronological and compact.
// A replay normally receives them in journal order; this function deliberately
// does not accumulate or sort them, because an observer needs to see the
// order work was recorded. The main agent has no task coordinates, while a
// child retains the same compact grammar as its live row without pretending a
// replay knows the planner title.
func formatWorkUpdatedLine(data protocol.WorkUpdatedData) string {
	state := compactAgentField(string(data.State), "working")
	phase := compactAgentField(string(data.Phase), "working")
	step := compactAgentField(data.Step, "updated")
	if data.Role == protocol.WorkRoleMain {
		return truncateAgentLine(fmt.Sprintf("◆ main · %s · %s: %s", phase, state, step), maxAgentStatusRunes)
	}
	index, total := data.Index, data.Total
	if index < 1 {
		index = 1
	}
	if total < index {
		total = index
	}
	model := compactAgentField(data.Model, "model unknown")
	effort := compactAgentField(data.Effort, "effort default")
	return truncateAgentLine(fmt.Sprintf("agent [%d/%d] · %s · %s · %s: %s",
		index, total, model, effort, state, step), maxAgentStatusRunes)
}
