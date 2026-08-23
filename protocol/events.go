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
	default:
		return nil
	}
	if text == "" {
		return fmt.Errorf("protocol: %s data.text must be non-empty", event)
	}
	return nil
}
