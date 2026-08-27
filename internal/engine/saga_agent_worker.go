package engine

import (
	"context"
	"fmt"
)

// AgentWorker does a chapter's work by running one agent turn.
//
// It is the only place the saga loop meets a model. Keeping it this thin is
// what lets SagaRunner's budget and doom-loop guards be tested without a
// provider, and what would let a chapter later be worked by a subagent or a
// different model without touching the loop.
type AgentWorker struct {
	Agent *Agent
}

// Work runs the chapter as a single turn and reports what it cost.
func (w AgentWorker) Work(ctx context.Context, chapter Chapter, goal string) (WorkResult, error) {
	if w.Agent == nil {
		return WorkResult{}, fmt.Errorf("saga: no agent to work chapter %d", chapter.Number)
	}

	// Measured as a difference rather than read from the session total. The
	// agent accumulates across the whole session, so charging a chapter the
	// running total would make every chapter look dearer than the last and
	// stop the saga early for money it had already counted.
	before := w.Agent.SessionCostUSD()
	err := w.Agent.RunTurn(ctx, chapterPrompt(chapter, goal))
	spent := w.Agent.SessionCostUSD() - before
	if spent < 0 {
		spent = 0
	}
	if err != nil {
		return WorkResult{CostUSD: spent}, err
	}
	return WorkResult{CostUSD: spent, Summary: chapter.Title}, nil
}

// chapterPrompt states the chapter and the goal it serves.
//
// Both, and only both: a chapter without its goal is an instruction out of
// context, and a goal without the chapter restates the whole project every
// turn and invites the model to do all of it at once.
func chapterPrompt(chapter Chapter, goal string) string {
	return fmt.Sprintf(`You are working one chapter of a longer effort.

Overall goal: %s

This chapter (%d): %s

Do only this chapter. Make the change, run whatever check proves it, and stop.
The chapter's quality gates run after you finish and will reject work that does
not build or pass its tests.`, goal, chapter.Number, chapter.Title)
}
