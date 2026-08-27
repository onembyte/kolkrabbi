package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
)

// CommandType is a language-neutral client-to-server command name.
type CommandType string

const (
	// CommandPermissionResolve resolves one pending permission request.
	CommandPermissionResolve CommandType = "permission.resolve"
	// CommandTurnCancel cancels one live turn by canonical identifier.
	CommandTurnCancel CommandType = "turn.cancel"
	// CommandTurnStart asks a session to run one turn. It is the command a
	// paired device needs in order to do more than watch (item 26, I26.7).
	CommandTurnStart CommandType = "turn.start"
)

var knownCommandTypes = []CommandType{
	CommandTurnStart,
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

// maxRemotePromptBytes bounds one remote prompt.
//
// A prompt is not a one-off cost: it enters the conversation and is carried in
// every later request to the provider, so an unbounded one is an unbounded bill
// as well as an unbounded request. 32 KiB is far past any prompt a person types
// on a phone and far short of anything that hurts.
const maxRemotePromptBytes = 32 * 1024

// TurnStartCommand carries what the session should be asked to do.
type TurnStartCommand struct {
	Prompt string `json:"prompt"`
}

func validateTurnStartCommand(raw json.RawMessage) error {
	var command TurnStartCommand
	if err := json.Unmarshal(raw, &command); err != nil {
		return fmt.Errorf("protocol: turn.start command: %w", err)
	}
	if strings.TrimSpace(command.Prompt) == "" {
		return fmt.Errorf("protocol: turn.start command prompt must not be empty")
	}
	if len(command.Prompt) > maxRemotePromptBytes {
		return fmt.Errorf("protocol: turn.start command prompt is %d bytes, over the %d limit",
			len(command.Prompt), maxRemotePromptBytes)
	}
	return nil
}
