package engine

import (
	"fmt"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// UndoResult is what one undo took back, in both halves.
type UndoResult struct {
	// Files restored to their state before the turn.
	Files []string
	// Messages removed from the conversation.
	Messages int
}

// Undo takes back the last turn: the files it changed and the conversation
// that produced them.
//
// Rewind restores files alone, and that leaves the model's history describing
// edits that are no longer on disk — a divergence the user cannot see and the
// model cannot detect, which the next turn then reasons from. Undo is the
// version that keeps the two in step.
//
// The order is deliberate. Files first, and if they cannot be restored the
// conversation is left exactly as it was: a half-undo that rewinds history
// while leaving the edits in place is the same divergence in the other
// direction, and the one that silently loses work.
func (a *Agent) Undo() (UndoResult, error) {
	if a.Ckpt == nil {
		return UndoResult{}, fmt.Errorf("checkpointing is not enabled, so there is no file half to undo")
	}

	restored, err := a.Ckpt.RewindLastTurn()
	if err != nil {
		return UndoResult{Files: restored}, err
	}

	dropped := a.trimLastTurn()
	return UndoResult{Files: restored, Messages: dropped}, nil
}

// trimLastTurn removes the most recent user message and everything after it,
// returning how many messages went.
//
// A turn starts at what the person said. Everything after it — the assistant's
// replies, its tool calls and their results — exists because of that message,
// so taking the turn back means taking all of it.
func (a *Agent) trimLastTurn() int {
	if a.Sess == nil {
		return 0
	}
	messages := a.Sess.GetMessages()
	start := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			start = i
			break
		}
	}
	if start < 0 {
		return 0
	}
	kept := make([]provider.Message, start)
	copy(kept, messages[:start])
	a.Sess.SetMessages(kept)
	a.save()
	return len(messages) - start
}
