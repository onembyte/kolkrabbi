package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/engine"
)

// resumeNow is /resume: lift the pause whatever the clock says and run the
// turn that was waiting, on the same model. It runs on the caller's turn
// goroutine in both surfaces, so the resumed turn is the one turn in flight.
func (a *app) resumeNow(ctx context.Context, ag *engine.Agent) {
	pending, ok := ag.Resume()
	if !ok {
		fmt.Fprintln(a.stdout, "nothing is paused")
		return
	}
	if strings.TrimSpace(pending) == "" {
		fmt.Fprintln(a.stdout, "◆ pause lifted; nothing was waiting to be sent")
		return
	}
	fmt.Fprintln(a.stdout, "◆ pause lifted; re-sending the turn that was waiting")
	if err := a.runInteractivePrompt(ctx, ag, pending); err != nil {
		fmt.Fprintf(a.stderr, "\033[31merror:\033[0m %v\n", err)
		writeAdvice(a.stderr, err)
	}
}

// armAutoResume gives the engine the surface's way of running a turn that
// comes back on its own and the session context its monitors live in, then
// starts one if the session opened paused.
func (a *app) armAutoResume(ctx context.Context, ag *engine.Agent, run func(pending string)) {
	ag.ResumeReady = run
	ag.WatchPauses(ctx)
}

// continueNow is /continue [n]: walk the chain the pause recommended from the
// nth equivalent (1 by default), switch the session there, and run the turn
// that was waiting on the new model. Nothing is switched unless asked.
func (a *app) continueNow(ctx context.Context, ag *engine.Agent, arg string) {
	from := 0
	if arg = strings.TrimSpace(arg); arg != "" {
		n, err := strconv.Atoi(arg)
		if err != nil || n < 1 {
			fmt.Fprintln(a.stdout, "usage: /continue [n] — n is the position in the pause's list, 1 by default")
			return
		}
		from = n - 1
	}
	pending, chosen, err := ag.ContinueOn(ctx, from)
	if err != nil {
		fmt.Fprintln(a.stdout, err)
		return
	}
	if strings.TrimSpace(pending) == "" {
		fmt.Fprintf(a.stdout, "◆ on %s now; nothing was waiting to be sent\n", chosen.Ref())
		return
	}
	if err := a.runInteractivePrompt(ctx, ag, pending); err != nil {
		fmt.Fprintf(a.stderr, "\033[31merror:\033[0m %v\n", err)
		writeAdvice(a.stderr, err)
	}
}
