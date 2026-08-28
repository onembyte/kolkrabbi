package agentcli

import (
	"encoding/json"
	"fmt"

	"github.com/onembyte/kolkrabbi/internal/secret"
)

type EventKind string

const (
	EventInit             EventKind = "init"
	EventMessageDelta     EventKind = "message.delta"
	EventMessageCompleted EventKind = "message.completed"
	EventTool             EventKind = "tool"
	EventUsage            EventKind = "usage"
	EventError            EventKind = "error"
)

// Event is the allow-listed, credential-free projection of one Claude frame.
type Event struct {
	Kind          EventKind
	Model         string
	SessionID     string
	Text          string
	InputTokens   int
	OutputTokens  int
	CacheRead     int
	CacheCreation int
	CostUSD       float64
	Error         string
	ToolName      string
	ToolInput     string
}

type wireFrame struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Model   string `json:"model"`
	// SessionID is the vendor's own conversation handle, echoed by
	// system/init and by the result frame. It is the --resume handle, the only
	// piece of vendor state worth keeping — and it names a conversation, not
	// a credential.
	SessionID string `json:"session_id"`
	Message   *struct {
		Model   string `json:"model"`
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
		Usage *wireUsage `json:"usage"`
	} `json:"message"`
	Result       string     `json:"result"`
	IsError      bool       `json:"is_error"`
	TotalCostUSD float64    `json:"total_cost_usd"`
	Usage        *wireUsage `json:"usage"`
}

type wireUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

// Translate converts one provider frame to an allow-listed event. Hooks,
// authentication frames, and unknown frames are intentionally dropped.
func Translate(line []byte) ([]Event, error) {
	var frame wireFrame
	if err := json.Unmarshal(line, &frame); err != nil {
		return nil, fmt.Errorf("invalid Claude stream frame: %w", err)
	}
	switch frame.Type {
	case "system":
		if frame.Subtype != "init" {
			return nil, nil
		}
		return []Event{{Kind: EventInit, Model: frame.Model, SessionID: frame.SessionID}}, nil
	case "assistant":
		if frame.Message == nil {
			return nil, nil
		}
		var events []Event
		for _, block := range frame.Message.Content {
			if block.Type == "text" && block.Text != "" {
				events = append(events, Event{Kind: EventMessageDelta, Model: frame.Message.Model, Text: secret.Scrub(block.Text)})
			}
			if block.Type == "tool_use" && block.Name != "" {
				events = append(events, Event{
					Kind: EventTool, ToolName: block.Name,
					ToolInput: secret.Scrub(string(block.Input)),
				})
			}
		}
		if frame.Message.Usage != nil {
			events = append(events, usageEvent(frame.Message.Model, frame.Message.Usage, 0))
		}
		return events, nil
	case "result":
		events := []Event{{
			Kind: EventMessageCompleted, Text: secret.Scrub(frame.Result), SessionID: frame.SessionID,
		}}
		if frame.Usage != nil || frame.TotalCostUSD != 0 {
			events = append(events, usageEvent(frame.Model, frame.Usage, frame.TotalCostUSD))
		}
		if frame.IsError {
			events = append(events, Event{Kind: EventError, Error: secret.Scrub(frame.Result)})
		}
		return events, nil
	default:
		return nil, nil
	}
}

func usageEvent(model string, usage *wireUsage, cost float64) Event {
	event := Event{Kind: EventUsage, Model: model, CostUSD: cost}
	if usage != nil {
		event.InputTokens = usage.InputTokens
		event.OutputTokens = usage.OutputTokens
		event.CacheRead = usage.CacheReadInputTokens
		event.CacheCreation = usage.CacheCreationInputTokens
	}
	return event
}
