package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/engine"
)

// repl is the interactive loop. It returns nil on EOF (Ctrl+D) or /exit; a
// failed turn is reported and the loop continues, because losing a session to
// one transport hiccup is the wrong trade.
// replPrompt opens a line in the plain REPL, matching the composer.
const replPrompt = "❯"

func (a *app) repl(ctx context.Context, ag *engine.Agent) error {
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
		ag.Mode, ag.Effort, ag.Model, permissionTag(ag.Permission), sessID, resumedNote)
	fmt.Fprintln(a.stdout, "Type your request, or /help for commands. Ctrl+C interrupts a turn, /exit quits.")

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

		if strings.HasPrefix(line, "/") {
			tctx, stop := signal.NotifyContext(ctx, os.Interrupt)
			shouldExit := a.slash(tctx, ag, line)
			stop()
			if shouldExit || eof {
				return nil
			}
			continue
		}

		// Per-turn interrupt: Ctrl+C cancels this turn only, not the REPL.
		tctx, stop := signal.NotifyContext(ctx, os.Interrupt)
		err = ag.RunTurn(tctx, line)
		stop()
		switch {
		case errors.Is(err, context.Canceled):
			fmt.Fprintln(a.stdout, "\033[2m(interrupted)\033[0m")
		case err != nil:
			fmt.Fprintf(a.stderr, "\033[31merror:\033[0m %v\n", err)
		}
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
