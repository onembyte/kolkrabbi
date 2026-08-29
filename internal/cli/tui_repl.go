package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/local"
	"github.com/onembyte/kolkrabbi/internal/paths"
	"github.com/onembyte/kolkrabbi/internal/projectfiles"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/term"
	"github.com/onembyte/kolkrabbi/internal/tui"
	"github.com/onembyte/kolkrabbi/protocol"
)

// paletteTier maps the terminal's own colour declarations to the TUI's escape
// tiers. Truecolor and 256-color terminals get the full palette; a plain
// 16-color one gets its nearest equivalents rather than garbage or nothing.
func paletteTier() string {
	colorTerm := strings.TrimSpace(strings.ToLower(os.Getenv("COLORTERM")))
	if strings.Contains(colorTerm, "truecolor") || strings.Contains(colorTerm, "24bit") {
		return "256"
	}
	termName := strings.TrimSpace(strings.ToLower(os.Getenv("TERM")))
	if strings.Contains(termName, "256color") ||
		strings.Contains(termName, "kitty") ||
		strings.Contains(termName, "alacritty") ||
		strings.Contains(termName, "wezterm") ||
		strings.Contains(termName, "ghostty") {
		return "256"
	}
	return "16"
}

// idempotentRestore wraps the CLI's raw-mode restore so every exit path — the
// happy path, a panic, and the defers — collapses to one actual restore. The
// screen hands back the terminal once; a second restore on an already-restored
// fd is at best redundant and at worst races a shell already reading it.
func idempotentRestore(restore func() error, err error) (func() error, error) {
	if err != nil {
		return nil, err
	}
	var once sync.Once
	return func() error {
		var restoreErr error
		once.Do(func() { restoreErr = restore() })
		return restoreErr
	}, nil
}

func (a *app) canUseTUI() bool {
	return a.terminalInput != nil && a.terminalOutput != nil &&
		a.canAnimate != nil && a.canAnimate() &&
		a.enterRaw != nil && a.terminalSize != nil
}

// tuiRepl binds the pure TUI runtime to one live engine session. The legacy
// line REPL remains untouched for pipes, redirected output, TERM=dumb, and
// tests that do not provide real terminal files.
func (a *app) tuiRepl(ctx context.Context, ag *engine.Agent) error {
	restoreTerminal, err := idempotentRestore(a.enterRaw(a.terminalInput))
	if err != nil {
		return err
	}
	// Raw mode, the hidden cursor, and paste framing are process state the
	// happy path below restores — but a panic unwinds on some other stack
	// entirely, and a signal kills the process wherever it happens to be.
	// Every path out of this function has to give the terminal back, or the
	// user is left in a shell that echoes nothing until they type `reset`.
	defer func() {
		if r := recover(); r != nil {
			_ = restoreTerminal()
			panic(r)
		}
	}()
	defer func() { _ = restoreTerminal() }()

	// One capability probe, made once: the terminal's colour tier cannot
	// change while kolk is attached to it, and a NO_COLOR user who opted out
	// of colour should not find purple SGR on every frame.
	if term.Color() {
		tui.SetPalette(paletteTier())
	} else {
		tui.SetPalette("none")
	}

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
		// Read on the spinner's tick: context and cost move during a turn, and a
		// footer that only updates between turns shows both frozen for exactly
		// as long as the user is watching them change.
		Meter: func() (string, string) { return contextLabel(ag), sessionCostLabel(ag) },
		Turn: func(turnContext context.Context, prompt string) error {
			if strings.HasPrefix(strings.TrimSpace(prompt), "/") {
				prompt = strings.TrimSpace(prompt)
				// `/model` with no argument is the picker, not a catalog dump: the
				// screen can offer every model with an effort dial alongside, so
				// a plain list is the worse answer. The plain REPL keeps its
				// catalog view; it has no arrow keys to offer.
				if picked, shown := tuiModelPickerCommand(turnContext, screen, a, ag, prompt); shown {
					if picked == "" {
						return nil // dismissed
					}
					prompt = picked
				}
				shouldExit := a.slash(turnContext, ag, prompt)
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
	// The count from A33.1's events, straight to the row above the composer. A
	// count nothing feeds reads zero forever and looks correct, which is the
	// one way this feature fails silently.
	ag.Agents = func(running int) { screen.Controller().SetAgents(running) }
	ag.Decider = tuiDecider{runtime: screen}
	ag.Ask = tuiChooser{runtime: screen}
	runErr := screen.Run(ctx)
	a.stdout, a.stderr = originalStdout, originalStderr
	restoreErr := restoreTerminal()
	// Only now: the renderer has released the screen and the terminal is out of
	// raw mode, so a failed exec leaves a usable shell rather than a dead one.
	if runErr == nil && restoreErr == nil {
		a.finishSession(ctx, ag)
	}
	return errors.Join(runErr, restoreErr)
}

// tuiModels feeds the picker from the snapshot startup already loaded. It used
// to re-read the catalog here, which on a stale cache meant a second network
// wait before the first prompt could be drawn.
func tuiModels(ctx context.Context, a *app, ag *engine.Agent) []tui.ModelSpec {
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
	//
	// Installed is not the same as signed in, and the row has to say which,
	// because selecting one that is merely installed is refused with "needs the
	// claude connector". A label that promises what selecting it cannot deliver
	// is worse than no label.
	var manifest provider.ConnectorManifest
	if d, err := a.locate(); err == nil {
		manifest, _ = provider.LoadConnectors(d.ConnectorsFile())
	}
	signedIn := func(plan provider.PlanModel) bool {
		for _, connector := range manifest.Connectors {
			if connector.Provider == plan.Provider && connector.Name == plan.Connector && connector.Enabled {
				return true
			}
		}
		return false
	}
	for _, plan := range provider.PlanModels("") {
		if !a.connectorInstalled(plan.Connector) {
			continue
		}
		if signedIn(plan) {
			out = append(out, tui.ModelSpec{
				ID: plan.Model, Cost: tui.CostSubscription, Rank: tui.ModelRank(tui.CostSubscription),
				Name: plan.Plan + " · via your " + plan.Connector + " login",
			})
			continue
		}
		out = append(out, tui.ModelSpec{
			ID: plan.Model, Cost: tui.CostSubscriptionLogin, Rank: tui.ModelRank(tui.CostSubscriptionLogin),
			Name: fmt.Sprintf("%s · sign in first:  kolk plans login %s %q", plan.Plan, plan.Provider, plan.Plan),
		})
	}

	// Local models already pulled onto this machine.
	// The user's own Ollama, as it actually is (E9): what is pulled, under the
	// ids the router understands. The rows this replaces were a static
	// catalogue of models nobody had pulled, under bare ids that went to the
	// gateway and 404'd.
	out = append(out, a.hostModelRows(ctx, manifest)...)

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

// tuiModelPickerCommand turns a bare `/model` into the picker's command. It
// returns the picked command (and shown=true) only on this screen and only
// when the argument was bare; a command with arguments, a screen that cannot
// show the picker, or a dismissal falls through with shown=false or picked="".
func tuiModelPickerCommand(ctx context.Context, screen *tui.Runtime, a *app, ag *engine.Agent, prompt string) (string, bool) {
	if strings.TrimSpace(prompt) != "/model" {
		return "", false
	}
	entries := tuiModelPickEntries(ctx, a, ag)
	picked, ok := screen.AskModel(ctx, entries)
	if !ok {
		return "", false
	}
	return picked, true
}

// tuiModelPickEntries is the /model suggestion list, one row per model with
// the effort dial of its plan where the plan offers one. The dial starts at
// the level the session already runs at — mapped down onto whatever the plan
// actually offers.
func tuiModelPickEntries(ctx context.Context, a *app, ag *engine.Agent) []tui.ModelPickEntry {
	specs := tuiModels(ctx, a, ag)
	current, _ := engine.NormalizeEffort(ag.Effort)
	out := make([]tui.ModelPickEntry, 0, len(specs))
	for _, spec := range specs {
		entry := tui.ModelPickEntry{ID: spec.ID, Name: spec.Name}
		for _, plan := range provider.PlanModels("") {
			if plan.Model != spec.ID {
				continue
			}
			entry.Efforts = plan.Efforts
			if resolved, _ := provider.EffortForPlan(current, plan.Efforts); resolved != "" {
				for index, effort := range plan.Efforts {
					if strings.EqualFold(effort, resolved) {
						entry.Effort = index
						break
					}
				}
			}
			break
		}
		out = append(out, entry)
	}
	return out
}

func tuiPlans() []tui.PlanSpec {
	plans := provider.Plans("")
	out := make([]tui.PlanSpec, 0, len(plans))
	for _, plan := range plans {
		out = append(out, tui.PlanSpec{Provider: plan.Provider, Name: plan.Name, Auth: plan.Auth})
	}
	return out
}

func tuiWelcome(messageCount int) string {
	var welcome strings.Builder
	welcome.WriteString("Type a request or /help. Up arrow recalls history; Esc stops a running turn; idle, Ctrl+C clears input, twice exits.\n")
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

// tuiChooser adapts the engine's question to the screen's picker. The engine
// does not know what a terminal is and the screen does not know what a model
// is, so the translation lives here, next to the other port adapters.
type tuiChooser struct{ runtime *tui.Runtime }

func (c tuiChooser) Choose(ctx context.Context, choice engine.Choice) (string, bool) {
	return c.runtime.Ask(ctx, tui.Question{Prompt: choice.Question, Options: choice.Options})
}

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

// hostModelRows lists the host's models for the picker. Local models are the
// local cost class, labelled with what the machine will run them on and
// whether they can take tools; cloud models bill against the Ollama plan, so
// their row is the plan's — subscription when the connector is verified,
// sign-in-first when not. An installed-but-idle Ollama lists nothing: starting
// a server to populate a picker is memory spent on a model nobody picked.
func (a *app) hostModelRows(ctx context.Context, manifest provider.ConnectorManifest) []tui.ModelSpec {
	if a.discoverHost == nil || a.listHostModels == nil {
		return nil
	}
	host := a.discoverHost(ctx)
	if host.State != local.HostRunning {
		return nil
	}
	cache := ""
	if d, err := a.locate(); err == nil {
		cache = d.HostCatalogFile()
	}
	models, err := a.listHostModels(ctx, host.Addr, cache)
	if err != nil {
		return nil
	}

	cloudVerified, plan := false, "Ollama Pro"
	for _, connector := range manifest.Connectors {
		if connector.Name == local.SidecarName && connector.Enabled {
			cloudVerified = connector.Verified
			if connector.Plan != "" {
				plan = connector.Plan
			}
		}
	}
	// The same bounded probe `kolk localia` uses; 112 µs on the owner's
	// machine, and a process exec where nvidia-smi exists.
	modelDir := ""
	if d, err := a.locate(); err == nil {
		modelDir = d.LocalModelsDir()
	}
	cpuOnly := len(a.hardware(ctx, modelDir).Accelerators) == 0

	rows := make([]tui.ModelSpec, 0, len(models))
	for _, m := range models {
		id := local.HostPrefix + m.Name
		if m.Cloud {
			if cloudVerified {
				rows = append(rows, tui.ModelSpec{
					ID: id, Cost: tui.CostSubscription, Rank: tui.ModelRank(tui.CostSubscription),
					Name: sizeLabel(m) + "cloud via ollama.com · " + plan,
				})
				continue
			}
			rows = append(rows, tui.ModelSpec{
				ID: id, Cost: tui.CostSubscriptionLogin, Rank: tui.ModelRank(tui.CostSubscriptionLogin),
				Name: fmt.Sprintf("%scloud via ollama.com · sign in first:  kolk plans login ollama %q", sizeLabel(m), plan),
			})
			continue
		}
		name := sizeLabel(m) + "runs on this machine"
		if cpuOnly {
			name += " · CPU only"
		}
		switch {
		case !m.CapabilitiesKnown:
			name += " · capabilities unknown"
		case !m.Tools:
			name += " · chat only, no tools"
		}
		rows = append(rows, tui.ModelSpec{ID: id, Cost: tui.CostLocal, Rank: tui.ModelRank(tui.CostLocal), Name: name})
	}
	return rows
}

func sizeLabel(m local.HostModel) string {
	parts := make([]string, 0, 2)
	if m.Parameters != "" {
		parts = append(parts, m.Parameters)
	}
	if m.Quantization != "" {
		parts = append(parts, m.Quantization)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ") + " · "
}
