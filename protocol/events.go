package protocol

import (
	"encoding/json"
	"fmt"
)

// EventType is the language-neutral event name carried in Envelope.Type.
// Decoders retain syntactically valid unknown values for forward compatibility.
type EventType string

const (
	// EventHello announces the protocol, server identity, and capabilities.
	EventHello EventType = "hello"
	// EventMessageDelta carries display-ready assistant text as it streams.
	EventMessageDelta EventType = "message.delta"
	// EventMessageCompleted carries the authoritative final assistant-text snapshot.
	EventMessageCompleted EventType = "message.completed"
	// EventReasoningDelta carries display-ready reasoning text as it streams.
	EventReasoningDelta EventType = "reasoning.delta"
	// EventToolRequested announces one complete tool invocation.
	EventToolRequested EventType = "tool.requested"
	// EventToolStarted announces that one requested invocation began execution.
	EventToolStarted EventType = "tool.started"
	// EventToolOutput carries the complete display-ready output of one invocation.
	EventToolOutput EventType = "tool.output"
	// EventSessionStarted announces the initial live-session projection.
	EventSessionStarted EventType = "session.started"
	// EventSessionUpdated carries a non-empty patch to the live-session projection.
	EventSessionUpdated EventType = "session.updated"
	// EventSessionEnded announces why a live session ended.
	EventSessionEnded EventType = "session.ended"
	// EventTurnStarted records the request projection used to begin a turn.
	EventTurnStarted EventType = "turn.started"
	// EventTurnFinished records why a turn completed.
	EventTurnFinished EventType = "turn.finished"
	// EventTurnCancelled records why a turn was cancelled.
	EventTurnCancelled EventType = "turn.cancelled"
)

// HelloData is the payload of EventHello and the future /v1/hello response.
type HelloData struct {
	Protocol     string   `json:"protocol"`
	Server       string   `json:"server"`
	Capabilities []string `json:"capabilities"`
}

// MessageDeltaData is the payload of EventMessageDelta.
type MessageDeltaData struct {
	Text string `json:"text"`
}

// MessageCompletedData is the payload of EventMessageCompleted.
type MessageCompletedData struct {
	Text string `json:"text"`
}

// ReasoningDeltaData is the payload of EventReasoningDelta.
type ReasoningDeltaData struct {
	Text string `json:"text"`
}

// ToolExecutor identifies who executes a requested tool.
type ToolExecutor string

const (
	// ToolExecutorKolk routes the invocation through Kolkrabbi's tool boundary.
	ToolExecutorKolk ToolExecutor = "kolk"
	// ToolExecutorProvider reports an invocation the backend already executed.
	ToolExecutorProvider ToolExecutor = "provider"
)

func validToolExecutor(executor ToolExecutor) bool {
	return executor == ToolExecutorKolk || executor == ToolExecutorProvider
}

// ToolRequestedData is the payload of EventToolRequested. Arguments retains
// the provider's complete JSON text without normalization.
type ToolRequestedData struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Arguments string       `json:"arguments"`
	Executor  ToolExecutor `json:"executor"`
}

// ToolStartedData is the payload of EventToolStarted.
type ToolStartedData struct {
	ID       string       `json:"id"`
	Executor ToolExecutor `json:"executor"`
}

// ToolOutputData is the payload of EventToolOutput. Output may be empty when
// the tool completed without display text.
type ToolOutputData struct {
	ID       string       `json:"id"`
	Output   string       `json:"output"`
	Executor ToolExecutor `json:"executor"`
}

// SessionStartedData is the payload of EventSessionStarted.
type SessionStartedData struct {
	Model  string `json:"model"`
	Mode   string `json:"mode"`
	Effort string `json:"effort"`
	CWD    string `json:"cwd"`
}

// SessionUpdatedData is the payload of EventSessionUpdated. At least one
// known or future field must be present in its wire object.
type SessionUpdatedData struct {
	Model  string `json:"model,omitempty"`
	Mode   string `json:"mode,omitempty"`
	Effort string `json:"effort,omitempty"`
	Title  string `json:"title,omitempty"`
}

// SessionEndedData is the payload of EventSessionEnded.
type SessionEndedData struct {
	Reason string `json:"reason"`
}

// TurnStartedData is the payload of EventTurnStarted.
type TurnStartedData struct {
	Input  string `json:"input"`
	Model  string `json:"model"`
	Mode   string `json:"mode"`
	Effort string `json:"effort"`
}

// TurnFinishedData is the payload of EventTurnFinished. RawReason preserves an
// optional provider-specific finish reason without restricting future values.
type TurnFinishedData struct {
	Reason    string `json:"reason"`
	RawReason string `json:"raw_reason,omitempty"`
}

// TurnCancelledData is the payload of EventTurnCancelled.
type TurnCancelledData struct {
	Reason string `json:"reason"`
}

func validateEventData(event EventType, raw json.RawMessage) error {
	var text string
	switch event {
	case EventHello:
		var data HelloData
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		if data.Protocol != Version {
			return fmt.Errorf("protocol: %s data.protocol must be %q", event, Version)
		}
		if data.Server == "" {
			return fmt.Errorf("protocol: %s data.server must be non-empty", event)
		}
		if data.Capabilities == nil {
			return fmt.Errorf("protocol: %s data.capabilities must be an array", event)
		}
		seen := make(map[string]struct{}, len(data.Capabilities))
		for _, capability := range data.Capabilities {
			if capability == "" {
				return fmt.Errorf("protocol: %s capability names must be non-empty", event)
			}
			if _, duplicate := seen[capability]; duplicate {
				return fmt.Errorf("protocol: %s capability %q is duplicated", event, capability)
			}
			seen[capability] = struct{}{}
		}
		return nil
	case EventMessageDelta:
		var data MessageDeltaData
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		text = data.Text
	case EventMessageCompleted:
		var data struct {
			Text *string `json:"text"`
		}
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		if data.Text == nil {
			return fmt.Errorf("protocol: %s data.text must be present and string-valued", event)
		}
		return nil
	case EventReasoningDelta:
		var data ReasoningDeltaData
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		text = data.Text
	case EventToolRequested:
		var data ToolRequestedData
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		for _, field := range []struct {
			name  string
			value string
		}{
			{name: "id", value: data.ID},
			{name: "name", value: data.Name},
			{name: "arguments", value: data.Arguments},
		} {
			if field.value == "" {
				return fmt.Errorf("protocol: %s data.%s must be non-empty", event, field.name)
			}
		}
		if !json.Valid([]byte(data.Arguments)) {
			return fmt.Errorf("protocol: %s data.arguments must contain valid JSON", event)
		}
		if !validToolExecutor(data.Executor) {
			return fmt.Errorf("protocol: %s data.executor must be %q or %q", event, ToolExecutorKolk, ToolExecutorProvider)
		}
		return nil
	case EventToolStarted:
		var data ToolStartedData
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		if data.ID == "" {
			return fmt.Errorf("protocol: %s data.id must be non-empty", event)
		}
		if !validToolExecutor(data.Executor) {
			return fmt.Errorf("protocol: %s data.executor must be %q or %q", event, ToolExecutorKolk, ToolExecutorProvider)
		}
		return nil
	case EventToolOutput:
		var data struct {
			ID       string       `json:"id"`
			Output   *string      `json:"output"`
			Executor ToolExecutor `json:"executor"`
		}
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		if data.ID == "" {
			return fmt.Errorf("protocol: %s data.id must be non-empty", event)
		}
		if data.Output == nil {
			return fmt.Errorf("protocol: %s data.output must be present and string-valued", event)
		}
		if !validToolExecutor(data.Executor) {
			return fmt.Errorf("protocol: %s data.executor must be %q or %q", event, ToolExecutorKolk, ToolExecutorProvider)
		}
		return nil
	case EventSessionStarted:
		var data SessionStartedData
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		for _, field := range []struct {
			name  string
			value string
		}{
			{name: "model", value: data.Model},
			{name: "mode", value: data.Mode},
			{name: "effort", value: data.Effort},
			{name: "cwd", value: data.CWD},
		} {
			if field.value == "" {
				return fmt.Errorf("protocol: %s data.%s must be non-empty", event, field.name)
			}
		}
		return nil
	case EventSessionUpdated:
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		if len(fields) == 0 {
			return fmt.Errorf("protocol: %s data must contain at least one field", event)
		}
		var data SessionUpdatedData
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		for _, field := range []struct {
			name  string
			value string
		}{
			{name: "model", value: data.Model},
			{name: "mode", value: data.Mode},
			{name: "effort", value: data.Effort},
			{name: "title", value: data.Title},
		} {
			if _, present := fields[field.name]; present && field.value == "" {
				return fmt.Errorf("protocol: %s data.%s must be non-empty", event, field.name)
			}
		}
		return nil
	case EventSessionEnded:
		var data SessionEndedData
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		if data.Reason == "" {
			return fmt.Errorf("protocol: %s data.reason must be non-empty", event)
		}
		return nil
	case EventTurnStarted:
		var data TurnStartedData
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		for _, field := range []struct {
			name  string
			value string
		}{
			{name: "input", value: data.Input},
			{name: "model", value: data.Model},
			{name: "mode", value: data.Mode},
			{name: "effort", value: data.Effort},
		} {
			if field.value == "" {
				return fmt.Errorf("protocol: %s data.%s must be non-empty", event, field.name)
			}
		}
		return nil
	case EventTurnFinished:
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		var data TurnFinishedData
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		if data.Reason == "" {
			return fmt.Errorf("protocol: %s data.reason must be non-empty", event)
		}
		if _, present := fields["raw_reason"]; present && data.RawReason == "" {
			return fmt.Errorf("protocol: %s data.raw_reason must be non-empty when present", event)
		}
		return nil
	case EventTurnCancelled:
		var data TurnCancelledData
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		if data.Reason == "" {
			return fmt.Errorf("protocol: %s data.reason must be non-empty", event)
		}
		return nil
	default:
		return nil
	}
	if text == "" {
		return fmt.Errorf("protocol: %s data.text must be non-empty", event)
	}
	return nil
}
