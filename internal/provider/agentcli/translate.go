package agentcli

import (
	"encoding/json"
	"fmt"
	"strings"

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
	EventLimit            EventKind = "limit"
)

// Event is the allow-listed, credential-free projection of one Claude frame.
// Every tool event is provider-executed: this backend owns no tool executor,
// so by the time a tool event exists the vendor has already run it.
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
	ToolCallID    string
	ToolInput     string
	ToolOutput    string
	ToolIsError   bool
	// Rate-limit data, when the frame was a rate_limit_event. Rejected means
	// the vendor refused the request outright; the consumer keeps it to
	// classify the terminal frame that follows.
	LimitRejected    bool
	LimitWindow      string
	LimitUtilization float64
	LimitResets      int64 // unix seconds
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
			// Tool-result blocks (user frames) reference the tool_use that
			// produced them and carry either a string or an array of blocks.
			ToolUseID  string          `json:"tool_use_id"`
			ToolResult json.RawMessage `json:"content"`
			IsError    bool            `json:"is_error"`
		} `json:"content"`
		Usage *wireUsage `json:"usage"`
	} `json:"message"`
	Result       string     `json:"result"`
	IsError      bool       `json:"is_error"`
	Errors       []string   `json:"errors"`
	TotalCostUSD float64    `json:"total_cost_usd"`
	Usage        *wireUsage `json:"usage"`
	// A rejected request does not end the stream — the terminal frame follows
	// it. The vendor sends this payload nested under rate_limit_info in
	// camelCase; that is the shape the captured fixture carries and the only
	// one ever observed on the wire.
	RateLimitInfo *wireRateLimit `json:"rate_limit_info"`
	// The flat snake_case spelling below was assumed before any fixture was
	// replayed, and no vendor frame has ever carried it. It is kept as a
	// tolerated fallback rather than deleted, because a frame this package
	// cannot read is a plan limit the user never hears about — but the nested
	// form wins, and it is the one the fixture test anchors.
	Status        string  `json:"status"`
	RateLimitType string  `json:"rate_limit_type"`
	Utilization   float64 `json:"utilization"`
	ResetsAt      int64   `json:"resets_at"`
}

type wireRateLimit struct {
	Status        string  `json:"status"`
	RateLimitType string  `json:"rateLimitType"`
	Utilization   float64 `json:"utilization"`
	ResetsAt      int64   `json:"resetsAt"`
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
					Kind: EventTool, ToolName: block.Name, ToolCallID: block.ID,
					ToolInput: secret.Scrub(string(block.Input)),
				})
			}
		}
		if frame.Message.Usage != nil {
			events = append(events, usageEvent(frame.Message.Model, frame.Message.Usage, 0))
		}
		return events, nil
	case "user":
		// A user frame carrying tool_result blocks is the vendor reporting what
		// it ran: the tool already executed, so the event is a record, never a
		// request. The frame does not repeat the tool's name — the id is all
		// there is, and the consumer matches it against the tool_use it saw.
		if frame.Message == nil {
			return nil, nil
		}
		var events []Event
		for _, block := range frame.Message.Content {
			if block.Type != "tool_result" {
				continue
			}
			events = append(events, Event{
				Kind: EventTool, ToolCallID: block.ToolUseID,
				ToolOutput:  secret.Scrub(flattenToolResult(block.ToolResult)),
				ToolIsError: block.IsError,
			})
		}
		return events, nil
	case "rate_limit_event":
		// A warning is the scarce resource speaking while there is still time to
		// notice; a rejection is the cause of a failure that has not arrived
		// yet. A plain "allowed" neither — dropped.
		status, window := frame.Status, frame.RateLimitType
		utilization, resets := frame.Utilization, frame.ResetsAt
		if frame.RateLimitInfo != nil {
			status = frame.RateLimitInfo.Status
			window = frame.RateLimitInfo.RateLimitType
			utilization = frame.RateLimitInfo.Utilization
			resets = frame.RateLimitInfo.ResetsAt
		}
		if status != "allowed_warning" && status != "rejected" {
			return nil, nil
		}
		return []Event{{
			Kind:             EventLimit,
			LimitRejected:    status == "rejected",
			LimitWindow:      secret.Scrub(window),
			LimitUtilization: utilization,
			LimitResets:      resets,
		}}, nil
	case "result":
		events := []Event{{
			Kind: EventMessageCompleted, Text: secret.Scrub(frame.Result), SessionID: frame.SessionID,
		}}
		if frame.Usage != nil || frame.TotalCostUSD != 0 {
			events = append(events, usageEvent(frame.Model, frame.Usage, frame.TotalCostUSD))
		}
		if frame.IsError {
			// Error subtypes (error_max_turns and friends) carry their prose in
			// errors[] and often no result at all — reading result alone
			// describes the failure as blank.
			text := strings.Join(frame.Errors, "; ")
			if text == "" {
				text = frame.Result
			}
			if frame.Subtype == "error_max_turns" {
				text = "the turn was cut off at the effort's round limit; the partial answer above is all that arrived: " + text
			}
			events = append(events, Event{Kind: EventError, Error: secret.Scrub(text)})
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

// flattenToolResult flattens a tool_result content field — which the vendor
// sends as either a JSON string or an array of content blocks — into one
// string. Shape-shifting output must never cost the whole frame.
func flattenToolResult(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	var out strings.Builder
	for _, block := range blocks {
		if block.Text == "" {
			continue
		}
		if out.Len() > 0 {
			out.WriteString("\n")
		}
		out.WriteString(block.Text)
	}
	return out.String()
}
