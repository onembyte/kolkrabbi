package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	folder := workingFolderLabel()
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
		Status:   tuiStatus(ag, "ready", folder),
		Commands: slashSuggestions(),
		Turn: func(turnContext context.Context, prompt string) error {
			if strings.HasPrefix(strings.TrimSpace(prompt), "/") {
				shouldExit := a.slash(turnContext, ag, strings.TrimSpace(prompt))
				screen.SetStatus(tuiStatus(ag, "ready", folder))
				if shouldExit {
					return tui.ErrExit
				}
				return nil
			}
			err := ag.RunTurn(turnContext, prompt)
			screen.SetStatus(tuiStatus(ag, "working", folder))
			if err != nil && !errors.Is(err, context.Canceled) {
				_, _ = fmt.Fprintf(screen, "\nerror: %v\n", err)
			}
			return err
		},
	})

	screen.Controller().AppendTranscript(tuiWelcome(len(ag.Sess.Messages)))

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

func tuiWelcome(messageCount int) string {
	var welcome strings.Builder
	welcome.WriteString("Type a request or /help. Up arrow recalls history; Ctrl+C clears input, twice exits.\n")
	if messageCount > 1 {
		_, _ = fmt.Fprintf(&welcome, "Resumed with %d messages.\n", messageCount-1)
	}
	return welcome.String()
}

func tuiStatus(ag *engine.Agent, lifecycle, folder string) tui.Status {
	approval := "ask"
	if ag.Yolo {
		approval = "auto"
	}
	model := ag.Model
	if tier, ok := ag.Tiers[ag.Effort]; ok && tier != "" {
		model = tier
	}
	return tui.Status{
		Model: model, Mode: ag.Mode, Effort: ag.Effort,
		Session: ag.Sess.ID, SessionName: ag.Sess.Title, Folder: folder,
		Approval: approval, Lifecycle: lifecycle,
	}
}

func workingFolderLabel() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "unknown"
	}
	home, _ := os.UserHomeDir()
	return compactWorkingFolder(cwd, home)
}

func compactWorkingFolder(cwd, home string) string {
	cwd = filepath.Clean(cwd)
	if home == "" {
		return cwd
	}
	relative, err := filepath.Rel(filepath.Clean(home), cwd)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return cwd
	}
	if relative == "." {
		return "~"
	}
	return filepath.Join("~", relative)
}

type tuiDecider struct{ runtime *tui.Runtime }

func (d tuiDecider) Confirm(ctx context.Context, confirmation engine.Confirmation) bool {
	return d.runtime.Confirm(ctx, tui.Approval{
		Action: confirmation.Action,
		Detail: confirmation.Detail,
	})
}
