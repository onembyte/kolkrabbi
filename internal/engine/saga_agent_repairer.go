package engine

import (
	"context"
	"fmt"
)

// AgentRepairer gives a chapter one turn to fix what its gates rejected.
//
// Separate from AgentWorker despite both being one turn, because they are told
// different things: the worker is asked to make a change, and this is asked to
// make a failing check pass without going further. A repair that quietly
// expands scope is how a bounded chapter stops being bounded.
type AgentRepairer struct {
	Agent *Agent
}

// Repair runs the fix-it turn.
func (r AgentRepairer) Repair(ctx context.Context, chapter Chapter, gateOutput string) error {
	if r.Agent == nil {
		return fmt.Errorf("saga: no agent to repair chapter %d", chapter.Number)
	}
	return r.Agent.RunTurn(ctx, repairPrompt(chapter, gateOutput))
}

func repairPrompt(chapter Chapter, gateOutput string) string {
	return fmt.Sprintf(`The work you just did for this chapter did not pass its quality gates.

Chapter %d: %s

What failed:
%s

Fix exactly this. Do not add features, refactor anything else, or widen the
change — the chapter is already written and only its regression is in question.
This is the one repair attempt; if the gates still fail afterwards the chapter's
changes are discarded entirely.`, chapter.Number, chapter.Title, gateOutput)
}
