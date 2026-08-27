package engine

import (
	"context"
	"fmt"
	"strings"
)

// donePhrase is what a planner says when the goal is met.
//
// A word rather than an empty reply, because a model that has nothing to add
// tends to explain itself at length rather than say nothing, and an empty
// string is indistinguishable from a failed call.
const donePhrase = "DONE"

// AgentPlanner chooses the next chapter by asking the model.
//
// It runs through the fast lane rather than the session model: choosing one
// next step from a short list of finished work is a cheap judgement, and paying
// the coding model for it once per chapter is how a saga's cost drifts away
// from the work it is actually doing.
type AgentPlanner struct {
	Agent *Agent
}

// Next returns the title of the next chapter, or "" when the goal is met.
func (p AgentPlanner) Next(ctx context.Context, goal string, done []Chapter) (string, error) {
	if p.Agent == nil {
		return "", fmt.Errorf("saga: no agent to plan with")
	}

	reply, err := p.Agent.FastLaneChat(ctx, sagaPlannerSystemPrompt, plannerPrompt(goal, done))
	if err != nil {
		return "", err
	}

	title := firstLine(reply)
	if strings.EqualFold(title, donePhrase) {
		return "", nil
	}
	return title, nil
}

const sagaPlannerSystemPrompt = `You choose the next single step of a long engineering task.

Answer with one short imperative line naming exactly one discrete, bounded change
— the kind that can be made and verified on its own. No numbering, no prose, no
explanation, no markdown.

If the goal is already met by the work listed, answer exactly: ` + donePhrase

// plannerPrompt states the goal and what the previous chapters achieved.
//
// Failed chapters are included with their verification message, and that is the
// point: repeating a chapter that just failed the same way is how a saga enters
// the loop the doom detector exists to stop, and the planner is the only thing
// that can avoid it.
func plannerPrompt(goal string, done []Chapter) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Goal: %s\n", goal)
	if len(done) == 0 {
		b.WriteString("\nNothing has been done yet. Name the first chapter.")
		return b.String()
	}
	b.WriteString("\nChapters so far:\n")
	for _, chapter := range done {
		fmt.Fprintf(&b, "%d. %s — %s", chapter.Number, chapter.Title, chapter.Status)
		if chapter.Verification != "" {
			fmt.Fprintf(&b, " (%s)", chapter.Verification)
		}
		b.WriteString("\n")
	}
	b.WriteString("\nName the next chapter.")
	return b.String()
}

// firstLine takes the one line a chapter title is allowed to be.
//
// "Exactly one discrete task" is the rule, and a multi-line answer is several
// tasks wearing one chapter. It also ends up in a commit message, where a
// newline is not welcome.
func firstLine(reply string) string {
	for _, line := range strings.Split(reply, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
