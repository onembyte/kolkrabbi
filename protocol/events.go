package protocol

import (
	"encoding/json"
	"fmt"
)

// EventType is the language-neutral event name carried in Envelope.Type.
// Decoders retain syntactically valid unknown values for forward compatibility.
type EventType string

const (
	// EventMessageDelta carries display-ready assistant text as it streams.
	EventMessageDelta EventType = "message.delta"
	// EventReasoningDelta carries display-ready reasoning text as it streams.
	EventReasoningDelta EventType = "reasoning.delta"
)

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
