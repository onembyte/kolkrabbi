package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/onembyte/kolkrabbi/internal/engine"
)

// runInteractivePrompt is the shared ordinary-turn boundary for interactive
// surfaces. An inline marker changes the workflow posture, not the user's
// message: the cleaned goal is persisted in SAGA.md and one wake runs on the
// same agent/session. Unmarked prompts retain the ordinary turn path.
func (a *app) runInteractivePrompt(ctx context.Context, ag *engine.Agent, prompt string) error {
	goal, marked := inlineSagaPrompt(prompt)
	if !marked {
		return ag.RunTurn(ctx, prompt)
	}
	if goal == "" {
		fmt.Fprintln(a.stdout, "use /saga inside your request, for example: build an ecommerce web app /saga")
		return nil
	}
	return a.runInlineSaga(ctx, ag, goal)
}

func (a *app) runInlineSaga(ctx context.Context, ag *engine.Agent, text string) (err error) {
	if ag == nil {
		return fmt.Errorf("saga: current agent is required")
	}
	opening, err := a.openSaga(text)
	if err != nil {
		return err
	}
	if opening.notice != "" {
		fmt.Fprintln(a.stdout, opening.notice)
	}
	if err := ag.SetPosture(engine.PostureSaga); err != nil {
		return err
	}
	defer func() {
		if restoreErr := ag.SetPosture(engine.Posture("")); restoreErr != nil {
			if err == nil {
				err = restoreErr
			} else {
				err = errors.Join(err, restoreErr)
			}
		}
	}()
	if a.sagaWake != nil {
		return a.sagaWake(ctx, ag)
	}
	return a.runSagaLoop(ctx, ag, opening.note)
}
