package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"

	"github.com/onembyte/kolkrabbi/internal/engine"
)

// repl is the interactive loop. It returns nil on EOF (Ctrl+D) or /exit; a
// failed turn is reported and the loop continues, because losing a session to
// one transport hiccup is the wrong trade.
// replPrompt opens a line in the plain REPL, matching the composer.
const replPrompt = "❯"

// replWriter keeps prompt/error writes whole while automatic delivery runs
// alongside the input reader. The turn lock cannot guard printing a prompt:
// waiting for a running turn there would prevent reading /exit or EOF.
type replWriter struct {
	mu  *sync.Mutex
	out io.Writer
}

func (w replWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.out.Write(p)
}

func (a *app) repl(ctx context.Context, ag *engine.Agent) error {
	stdout, stderr, engineOut := a.stdout, a.stderr, ag.Out
	var outputMu sync.Mutex
	a.stdout, a.stderr, ag.Out = replWriter{&outputMu, stdout}, replWriter{&outputMu, stderr}, replWriter{&outputMu, engineOut}
	if a.localRuntime != nil {
		a.localRuntime.SetOutput(a.stdout)
		defer a.localRuntime.SetOutput(stdout)
	}
	defer func() { a.stdout, a.stderr, ag.Out = stdout, stderr, engineOut }()
	resumedNote := ""
	if ag.Sess != nil {
		if n := len(ag.Sess.GetMessages()); n > 1 {
			resumedNote = fmt.Sprintf("  (resumed, %d messages)", n-1)
		}
	}
	sessID := ""
	if ag.Sess != nil {
		sessID = ag.Sess.SessionID()
	}
	fmt.Fprintf(a.stdout, "kolk — mode: %s · effort: %s · model: %s%s\nsession: %s%s\n",
		ag.Mode, ag.Effort, ag.SessionModel(), permissionTag(ag.Permission), sessID, resumedNote)
	fmt.Fprintln(a.stdout, "Type your request, or /help for commands. Ctrl+C interrupts a turn, /exit quits.")
	// A turn the resume monitor brings back runs on its goroutine, under the
	// same lock as a typed one, so the two never share the backend.
	var turnMu sync.Mutex
	stopResume := a.armAutoResume(ctx, ag, func(resumeCtx context.Context, pending string) bool {
		turnMu.Lock()
		defer turnMu.Unlock()
		if resumeCtx.Err() != nil {
			return false
		}
		turnCtx, stop := signal.NotifyContext(resumeCtx, os.Interrupt)
		defer stop()
		if err := a.runInteractivePrompt(turnCtx, ag, pending); err != nil {
			fmt.Fprintf(a.stderr, "\033[31merror:\033[0m %v\n", err)
		}
		return true
	})
	defer stopResume()

	for {
		// The same marker the persistent composer draws. Mode moved into the
		// banner and /mode rather than being repeated on every prompt.
		fmt.Fprintf(a.stdout, "\n\033[1m%s\033[0m ", replPrompt)
		line, err := a.in.ReadString('\n')
		// ReadString returns the final line AND io.EOF together when input ends
		// without a trailing newline, so returning on err would silently drop
		// the last command of any piped script.
		eof := errors.Is(err, io.EOF)
		if err != nil && !eof {
			return fmt.Errorf("reading input: %w", err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			if eof {
				return nil
			}
			continue
		}

		// `/saga` is an inline marker, including when it begins the line. A
		// non-empty marked request must beat slash dispatch or `/saga build`
		// would be treated as an unknown command instead of a normal goal.
		goal, markedSaga := inlineSagaPrompt(line)
		sagaRequest := markedSaga && goal != ""
		if looksLikeSlashCommand(line) && !sagaRequest {
			switch strings.Fields(line)[0] {
			case "/exit", "/quit":
				// Cancel a delivered turn before waiting for its turn lock.
				stopResume()
			case "/new", "/clear":
				// The same, keeping auto-resume for the new session: a
				// delivery waiting on the turn lock would otherwise never be
				// joined by the session swap that holds it.
				ag.QuiesceResume()
			}
			tctx, stop := signal.NotifyContext(ctx, os.Interrupt)
			turnMu.Lock()
			shouldExit := a.slash(tctx, ag, line)
			turnMu.Unlock()
			stop()
			if shouldExit || eof {
				return nil
			}
			continue
		}

		// Everything else is an ordinary request, and there is one boundary
		// for those: runInteractivePrompt decides marked from unmarked and
		// runs the turn. This branch used to be two — one that called the
		// boundary and one that called RunTurn directly — with the same
		// interrupt-and-error block copied under each.
		//
		// Per-turn interrupt: Ctrl+C cancels this turn only, not the REPL.
		tctx, stop := signal.NotifyContext(ctx, os.Interrupt)
		turnMu.Lock()
		err = a.runInteractivePrompt(tctx, ag, line)
		stop()
		switch {
		case errors.Is(err, context.Canceled):
			fmt.Fprintln(a.stdout, "\033[2m(interrupted)\033[0m")
		case err != nil:
			fmt.Fprintf(a.stderr, "\033[31merror:\033[0m %v\n", err)
			writeAdvice(a.stderr, err)
		}
		turnMu.Unlock()
		if eof {
			return nil
		}
	}
}

// permissionTag names the tier in the banner when it is anything but the safe
// default, so a session that will not stop to ask says so before it starts.
func permissionTag(p engine.Permission) string {
	if p == "" || p == engine.PermissionAsk {
		return ""
	}
	return "  (" + string(p) + ")"
}
