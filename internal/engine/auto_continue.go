package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/continuity"
)

// maxHopsPerRun bounds the automatic chain: a run that keeps hitting limits
// stops walking and pauses, rather than circling every configured model.
const maxHopsPerRun = 3

// autoContinue is plan 35 §2.4 with the mode on: under `select auto` or
// `preferred` the chain is walked at once; under `select ask` the block
// becomes one question per run — the top equivalent, the next, or pause and
// resume later. It returns the turn to re-run on the new model, or false to
// leave the pause standing. Off, the default, never continues.
func (a *Agent) autoContinue(ctx context.Context, pause continuity.Pause) (string, bool) {
	if !strings.EqualFold(a.ContinuityMode, "on") || a.Switch == nil || ctx.Err() != nil {
		return "", false
	}
	if a.hopsThisRun >= maxHopsPerRun {
		fmt.Fprintf(a.Out, "◆ %d hops this run already; pausing here rather than circling\n", a.hopsThisRun)
		return "", false
	}
	from := 0
	if strings.EqualFold(a.Select, "ask") {
		if a.askedThisRun || a.Ask == nil {
			return "", false
		}
		a.askedThisRun = true
		chain := a.chain(pause.Limit())
		if len(chain) == 0 {
			return "", false
		}
		options := []string{chain[0].Label()}
		if len(chain) > 1 {
			options = append(options, chain[1].Label())
		}
		options = append(options, "pause and resume later")
		answer, ok := a.Ask.Choose(ctx, Choice{Question: fmt.Sprintf("%s hit its %s. Continue on:", pause.Model, pause.HumanKind()), Options: options})
		switch {
		case !ok, strings.HasPrefix(answer, "pause"):
			fmt.Fprintln(a.Out, "◆ pausing as asked; kolk resumes at the reset, /continue walks the chain")
			return "", false
		case len(chain) > 1 && answer == options[1]:
			from = 1
		}
	}
	pending, _, err := a.ContinueOn(ctx, from)
	if err != nil {
		fmt.Fprintf(a.Out, "◆ %v\n", err)
		return "", false
	}
	a.hopsThisRun++
	return pending, true
}
