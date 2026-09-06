package cli

import (
	"context"
	"fmt"
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
