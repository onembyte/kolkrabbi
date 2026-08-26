package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/paths"
	"github.com/onembyte/kolkrabbi/internal/projectfiles"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/tui"
	"github.com/onembyte/kolkrabbi/protocol"
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

	// While the runtime below is live it reads the terminal from its own
	// goroutine. Anything that would hand the terminal to a child process must
	// see that Kolkrabbi owns it.
	a.terminalOwned = func() bool { return true }
	defer func() { a.terminalOwned = nil }()

	models := tuiModels(ctx, a, ag)
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
		Models:   models,
		Plans:    tuiPlans(),
		// Listed once at startup: a walk per keystroke would be the completion
		// making the composer feel slow, which is the opposite of the point.
		Files: projectfiles.List(projectRoot(), mentionCandidates),
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

	msgCount := 0
	if ag.Sess != nil {
		msgCount = len(ag.Sess.GetMessages())
	}
	screen.Controller().AppendTranscript(tuiWelcome(msgCount))

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

func tuiModels(ctx context.Context, a *app, ag *engine.Agent) []tui.ModelSpec {
	if ag.Client == nil {
		return nil
	}
	d, err := a.locate()
	if err != nil {
		return nil
	}
	models, err := ag.Client.ListModelsCached(ctx, d.CatalogFile(), provider.DefaultCatalogTTL, false)
	if err != nil {
		models = provider.FallbackCatalogSeed()
	}
	out := make([]tui.ModelSpec, 0, len(models))
	for _, model := range models {
		out = append(out, tui.ModelSpec{ID: model.ID, Name: model.Name})
	}
	return out
}

func tuiPlans() []tui.PlanSpec {
	plans := provider.Plans("")
	out := make([]tui.PlanSpec, 0, len(plans))
	for _, plan := range plans {
		out = append(out, tui.PlanSpec{Provider: plan.Provider, Name: plan.Name})
	}
	return out
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
	// The status line carries the tier verbatim: a session that will not stop
	// to ask should say so where the user is already looking.
	approval := string(ag.Permission)
	if approval == "" {
		approval = string(engine.DefaultPermission)
	}
	model := ag.ModelForEffort(ag.Effort)
	sessID, sessTitle := "", ""
	if ag.Sess != nil {
		sessID = ag.Sess.SessionID()
		sessTitle = ag.Sess.SessionTitle()
	}
	return tui.Status{
		Model: model, Mode: ag.Mode, Effort: ag.Effort,
		Session: sessID, SessionName: sessTitle, Folder: folder,
		Approval: approval, Lifecycle: lifecycle,
	}
}

func workingFolderLabel() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "unknown"
	}
	home, _ := paths.UserHomeDir()
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

// mentionCandidates bounds the completion list. Past a few hundred entries a
// filtered menu is not helping anyone find a file.
const mentionCandidates = 400

type tuiDecider struct{ runtime *tui.Runtime }

func (d tuiDecider) Confirm(ctx context.Context, confirmation engine.Confirmation) bool {
	decision := d.Decide(ctx, confirmation)
	return decision == protocol.PermissionDecisionAllow || decision == protocol.PermissionDecisionAllowSession
}

// Decide carries the TUI's three answers through to the engine. Without it the
// overlay's "always" would arrive as a plain yes and the rule the user read
// before agreeing to it would be dropped on the way.
func (d tuiDecider) Decide(ctx context.Context, confirmation engine.Confirmation) protocol.PermissionDecision {
	switch d.runtime.Decide(ctx, tui.Approval{
		Action: confirmation.Action,
		Detail: confirmation.Detail,
		Rule:   confirmation.Rule,
	}) {
	case tui.DecisionAllow:
		return protocol.PermissionDecisionAllow
	case tui.DecisionAllowAlways:
		return protocol.PermissionDecisionAllowSession
	default:
		return protocol.PermissionDecisionDeny
	}
}
