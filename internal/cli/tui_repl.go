package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/local"
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
	// The screen follows the window for as long as this runtime owns it. Until
	// now the layout was probed once and a widened terminal kept an 80-column
	// prompt in its left third.
	var resize <-chan struct{}
	if a.resizeNotifier != nil {
		changes, stop := a.resizeNotifier(a.terminalOutput)
		defer stop()
		resize = changes
	}
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
		Resize:   resize,
		Status:   tuiStatus(ag, "ready", folder),
		Commands: slashSuggestions(),
		Models:   models,
		Plans:    tuiPlans(),
		Settings: tuiSettings(a),
		// Listed once at startup: a walk per keystroke would be the completion
		// making the composer feel slow, which is the opposite of the point.
		Files: projectfiles.List(projectRoot(), mentionCandidates),
		// Shift+Tab walks the same three tiers /permissions lists, in the same
		// order, and lasts exactly as long as this session does.
		CyclePermission: func() string {
			ag.Permission = nextPermission(ag.Permission)
			return string(ag.Permission)
		},
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
				writeAdvice(screen, err)
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
	// Only now: the renderer has released the screen and the terminal is out of
	// raw mode, so a failed exec leaves a usable shell rather than a dead one.
	if runErr == nil && restoreErr == nil {
		a.performRestart(ag)
	}
	return errors.Join(runErr, restoreErr)
}

// tuiModels feeds the picker from the snapshot startup already loaded. It used
// to re-read the catalog here, which on a stale cache meant a second network
// wait before the first prompt could be drawn.
func tuiModels(_ context.Context, a *app, ag *engine.Agent) []tui.ModelSpec {
	if ag.Client == nil {
		return nil
	}
	// Every model the user can actually reach, in one list, each labelled by
	// what choosing it costs. Ordered so the ones that bill nothing extra come
	// first: a subscription already paid for, then free, then the user's own
	// hardware, and only then metered API rows.
	out := make([]tui.ModelSpec, 0, len(a.catalog)+16)

	// Subscriptions, but only where the provider's CLI is actually installed:
	// offering Claude Max on a machine with no claude binary is an instruction
	// that cannot be followed.
	for _, plan := range provider.PlanModels("") {
		if !a.connectorInstalled(plan.Connector) {
			continue
		}
		out = append(out, tui.ModelSpec{
			ID: plan.Model, Cost: tui.CostSubscription, Rank: tui.ModelRank(tui.CostSubscription),
			Name: plan.Plan + " · via your " + plan.Connector + " login",
		})
	}

	// Local models already pulled onto this machine.
	for _, entry := range local.Catalog("") {
		out = append(out, tui.ModelSpec{
			ID: entry.Name, Cost: tui.CostLocal, Rank: tui.ModelRank(tui.CostLocal),
			Name: entry.Parameters + " " + entry.Quantization + " · runs on this machine",
		})
	}

	models := a.catalog
	if len(models) == 0 {
		models = provider.FallbackCatalogSeed()
	}
	for _, model := range models {
		cost := tui.CostMetered
		if provider.ModelIsFree(model) {
			cost = tui.CostFree
		}
		out = append(out, tui.ModelSpec{
			ID: model.ID, Name: model.Name, Cost: cost, Rank: tui.ModelRank(cost),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Rank < out[j].Rank })
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
		return welcome.String()
	}
	// Only on a new session. The status line already shows which mode, effort
	// and model this session is on; what it cannot show is that they are
	// changeable mid-conversation, which is the thing a first-time user has no
	// way to discover and the thing that makes the three dials worth having.
	// A resumed session has met them already, and repeating it every time is
	// how an orientation becomes noise.
	welcome.WriteString("Switch anytime with /mode, /effort or /model. Each lists its options.\n")
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
		Context: contextLabel(ag), Cost: sessionCostLabel(ag),
	}
}

// contextLabel is how full the window is, or nothing before the first turn.
// "context 0%" would be a measurement nobody made.
func contextLabel(ag *engine.Agent) string {
	usage := ag.Context()
	if !usage.Measured {
		return ""
	}
	return fmt.Sprintf("%d%%", int(usage.Fraction()*100))
}

// sessionCostLabel is what this session has cost, once it has cost anything.
func sessionCostLabel(ag *engine.Agent) string {
	total := ag.SessionCostUSD()
	if total <= 0 {
		return ""
	}
	return fmt.Sprintf("$%.2f", total)
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

// permissionCycle is the order Shift+Tab walks: least to most autonomous, then
// back to asking. Wrapping to `ask` rather than stopping at `full-auto` means
// the key can always undo itself without the user reaching for a command.
var permissionCycle = [...]engine.Permission{
	engine.PermissionAsk,
	engine.PermissionAutoApprove,
	engine.PermissionFullAuto,
}

func nextPermission(current engine.Permission) engine.Permission {
	for index, tier := range permissionCycle {
		if tier == current {
			return permissionCycle[(index+1)%len(permissionCycle)]
		}
	}
	// An unset or unrecognised tier is treated as the default, so the first
	// press moves somewhere predictable instead of somewhere arbitrary.
	return permissionCycle[1]
}

// tuiSettings feeds the /config picker. Values are the ones in effect, so the
// list answers "what is kolk doing" without leaving the session.
func tuiSettings(a *app) []tui.SettingSpec {
	d, err := a.locate()
	if err != nil {
		return nil
	}
	cfg, err := config.Load(d.ConfigFile())
	if err != nil {
		return nil
	}
	rows := cfg.Settings(defaultModel, provider.DefaultBaseURL)
	out := make([]tui.SettingSpec, 0, len(rows))
	for _, row := range rows {
		out = append(out, tui.SettingSpec{
			Key: row.Key, Value: row.Value, Summary: row.Summary, Default: row.Default,
		})
	}
	return out
}
