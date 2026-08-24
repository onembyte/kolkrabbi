package protocol

import (
	"encoding/json"
	"fmt"
)

// CommandType is a language-neutral client-to-server command name.
type CommandType string

const (
	// CommandPermissionResolve resolves one pending permission request.
	CommandPermissionResolve CommandType = "permission.resolve"
	// CommandTurnCancel cancels one live turn by canonical identifier.
	CommandTurnCancel CommandType = "turn.cancel"
)

var knownCommandTypes = []CommandType{
	CommandTurnCancel,
	CommandPermissionResolve,
}

// KnownCommandTypes returns the ordered command vocabulary shipped by this
// protocol binding. The returned slice is a copy and may be modified freely.
func KnownCommandTypes() []CommandType {
	return append([]CommandType(nil), knownCommandTypes...)
}

// PermissionResolveCommand correlates one pending permission request with the
// client's decision. Server-owned resolution reasons are emitted later.
type PermissionResolveCommand struct {
	ID       string             `json:"id"`
	Decision PermissionDecision `json:"decision"`
}

func validatePermissionResolveCommand(raw json.RawMessage) error {
	var command PermissionResolveCommand
	if err := json.Unmarshal(raw, &command); err != nil {
		return fmt.Errorf("protocol: permission.resolve command: %w", err)
	}
	if command.ID == "" {
		return fmt.Errorf("protocol: permission.resolve command id must be non-empty")
	}
	if !validPermissionDecision(command.Decision) {
		return fmt.Errorf("protocol: permission.resolve command decision is not defined")
	}
	return nil
}

// TurnCancelCommand identifies the live turn the client wants to cancel.
type TurnCancelCommand struct {
	TurnID string `json:"turn_id"`
}

func validateTurnCancelCommand(raw json.RawMessage) error {
	var command TurnCancelCommand
	if err := json.Unmarshal(raw, &command); err != nil {
		return fmt.Errorf("protocol: turn.cancel command: %w", err)
	}
	if !validID(command.TurnID, 't') {
		return fmt.Errorf("protocol: turn.cancel command turn_id must be a canonical t_ ULID")
	}
	return nil
}
