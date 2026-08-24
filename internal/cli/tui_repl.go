package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/tui"
)

func (a *app) canUseTUI() bool {
	return a.terminalInput != nil && a.terminalOutput != nil &&
		a.canAnimate != nil && a.canAnimate() &&
		a.enterRaw != nil && a.terminalSize != nil
}

// tuiRepl binds the pure TUI runtime to one live engine session. The legacy
// line REPL remains untouched for pipes, redirected output, TERM=dumb, and
// tests that do not provide real terminal files.
func (a *app) tuiRepl(ctx context.Context, ag *engine.Agent) error {
	restoreTerminal, err := a.enterRaw(a.terminalInput)
	if err != nil {
		return err
	}

	originalStdout, originalStderr := a.stdout, a.stderr
	var screen *tui.Runtime
	screen = tui.NewRuntime(tui.RuntimeOptions{
		Input: a.terminalInput, Output: originalStdout,
		Width: func() int {
			width, _ := a.terminalSize(a.terminalOutput)
			return width
		},
		Height: func() int {
			_, height := a.terminalSize(a.terminalOutput)
			return height
		},
		Status:   tuiStatus(ag, "ready"),
		Commands: slashSuggestions(),
		Turn: func(turnContext context.Context, prompt string) error {
			if strings.HasPrefix(strings.TrimSpace(prompt), "/") {
				shouldExit := a.slash(turnContext, ag, strings.TrimSpace(prompt))
				screen.SetStatus(tuiStatus(ag, "ready"))
				if shouldExit {
					return tui.ErrExit
				}
				return nil
			}
			err := ag.RunTurn(turnContext, prompt)
			if err != nil && !errors.Is(err, context.Canceled) {
				_, _ = fmt.Fprintf(screen, "\nerror: %v\n", err)
			}
			return err
		},
	})

	resumedNote := ""
	if n := len(ag.Sess.Messages); n > 1 {
		resumedNote = fmt.Sprintf("  (resumed, %d messages)", n-1)
	}
	screen.Controller().AppendTranscript(fmt.Sprintf(
		"kolk — mode: %s · effort: %s · model: %s%s\nsession: %s%s\nType your request, or /help for commands. ↑ recalls history; Ctrl+C clears input, twice exits.\n",
		ag.Mode, ag.Effort, ag.Model, yoloTag(ag.Yolo), ag.Sess.ID, resumedNote,
	))

	a.stdout, a.stderr = screen, screen
	ag.Out = screen
	ag.Activity = screen
	ag.Work = screen
	ag.Decider = tuiDecider{runtime: screen}
	runErr := screen.Run(ctx)
	a.stdout, a.stderr = originalStdout, originalStderr
	restoreErr := restoreTerminal()
	return errors.Join(runErr, restoreErr)
}

func tuiStatus(ag *engine.Agent, lifecycle string) tui.Status {
	approval := "ask"
	if ag.Yolo {
		approval = "auto"
	}
	model := ag.Model
	if tier, ok := ag.Tiers[ag.Effort]; ok && tier != "" {
		model = tier
	}
	return tui.Status{
		Model: model, Mode: ag.Mode, Effort: ag.Effort, Session: ag.Sess.ID,
		Approval: approval, Lifecycle: lifecycle,
	}
}

type tuiDecider struct{ runtime *tui.Runtime }

func (d tuiDecider) Confirm(ctx context.Context, confirmation engine.Confirmation) bool {
	return d.runtime.Confirm(ctx, tui.Approval{
		Action: confirmation.Action,
		Detail: confirmation.Detail,
	})
}
