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
	// EventReasoningDelta carries display-ready reasoning text as it streams.
	EventReasoningDelta EventType = "reasoning.delta"
	// EventSessionStarted announces the initial live-session projection.
	EventSessionStarted EventType = "session.started"
	// EventSessionUpdated carries a non-empty patch to the live-session projection.
	EventSessionUpdated EventType = "session.updated"
	// EventSessionEnded announces why a live session ended.
	EventSessionEnded EventType = "session.ended"
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

// ReasoningDeltaData is the payload of EventReasoningDelta.
type ReasoningDeltaData struct {
	Text string `json:"text"`
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
	case EventReasoningDelta:
		var data ReasoningDeltaData
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("protocol: %s data: %w", event, err)
		}
		text = data.Text
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
	default:
		return nil
	}
	if text == "" {
		return fmt.Errorf("protocol: %s data.text must be non-empty", event)
	}
	return nil
}
