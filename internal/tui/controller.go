package tui

import (
	"fmt"
	"sort"
	"strings"
)

// interruptExitNotice is the idle-state Ctrl+C message: the same two-stage
// contract as before. A busy turn changes what the key does.
const interruptExitNotice = "Input cleared. Press Ctrl+C again to exit."

// Decision is the result of one visible approval overlay.
type Decision uint8

const (
	DecisionNone Decision = iota
	DecisionDeny
	DecisionAllow
	// DecisionAllowAlways allows this action and keeps the rule shown in the
	// overlay. It is a separate decision from DecisionAllow because the user
	// agreed to a different, wider thing.
	DecisionAllowAlways
)

// Effect asks the runtime to perform work outside the pure controller.
type Effect struct {
	Submit    string
	Interrupt bool
	Exit      bool
	Decision  Decision
	// Choice is the option picked from a question, 1-based. Zero means no
	// choice was made by this key.
	Choice int
	// ChoiceDismissed reports a question closed without an answer, which is a
	// different thing from picking the first option.
	ChoiceDismissed bool
	// PickModel is the command line the /model picker resolved to, or empty.
	// PickDismissed distinguishes closing the picker untouched from a pick.
	PickModel     string
	PickDismissed bool
	// PickConfig is the setting key the /config picker resolved to. Unlike
	// PickModel it is never itself a command to run — a setting still needs
	// its value typed — so resolving it fills the composer's draft instead.
	PickConfig string
	// CyclePermission asks the surface to advance the permission tier. The
	// controller cannot do it itself: what the tiers are, and what changing
	// one means, belongs to the engine rather than to a screen.
	CyclePermission bool
	// Secret is what the masked overlay delivered on Enter; SecretSubmitted
	// says Enter happened even when the value is empty, and SecretDismissed
	// says the overlay was closed without a value. None of it is ever echoed.
	Secret          string
	SecretSubmitted bool
	SecretDismissed bool
}

// Approval is the independent permission overlay. Input never shares storage
// with the main composer draft.
type Approval struct {
	Action string
	Detail string
	Input  string
	// Rule is the standing rule `a` would keep. It is shown in full: an
	// approval whose scope the user cannot read is not one they gave. Empty
	// means there is nothing to propose and `a` is not offered.
	Rule string
}

// Controller applies terminal and engine events to one screen and editor.
type Controller struct {
	screen *Model
	// question is the open picker, if any. It takes keys ahead of the composer
	// for the same reason the approval overlay does: what is on screen is a
	// question, and the next key is its answer.
	question      *Question
	questionIndex int
	// lastOptions outlives the question by one key, because the Effect that
	// closes it carries an index that still has to be resolved to an answer.
	lastOptions    []string
	beforeQuestion string
	// modelPicker is the open /model overlay, if any. It takes keys ahead of
	// the composer like the question overlay does, and resolves to a command
	// line rather than to an option string.
	modelPicker []ModelPickEntry
	modelIndex  int
	// modelFilter narrows modelPicker to the rows fuzzyScoreFields still
	// accepts. Index is into that filtered, ranked view, not into modelPicker
	// itself — the two only coincide while the filter is empty.
	modelFilter filterBox
	// modelTop is the first row of the filtered view on screen, kept in
	// bounds by scrollWindow the same way suggestionTop is — a catalog longer
	// than the window would otherwise render every row unbounded.
	modelTop int
	// configPicker is the open /config overlay, if any — the same shape as
	// modelPicker, minus the effort dial, resolving to a setting key rather
	// than a ready-to-run command.
	configPicker  []SettingSpec
	configIndex   int
	configFilter  filterBox
	configTop     int
	editor        *Editor
	status        Status
	agentStatuses map[string]AgentStatus
	busy          bool
	// queued holds a request submitted while a turn was still running. The
	// engine session is stateful, so two turns cannot run at once; the request
	// waits here and starts the moment the running one finishes.
	queued          string
	approval        *Approval
	approvalEditor  *Editor
	beforeApproval  string
	secret          *SecretPrompt
	secretEditor    *Editor
	beforeSecret    string
	commands        []CommandSpec
	models          []ModelSpec
	plans           []PlanSpec
	commandHistory  *CommandHistory
	files           []string
	suggestionLimit int
	suggestions     []CommandSpec
	suggestionIndex int
	// suggestionTop is the first row on screen. The selection moves through the
	// whole list; this follows it, so a catalog longer than the window is
	// reachable by holding an arrow key.
	suggestionTop  int
	settings       []SettingSpec
	interruptArmed bool
}

// NewController returns a ready controller with an empty transcript and draft.
func NewController(status Status, maxDraftRunes int) *Controller {
	return &Controller{
		screen: New(status), editor: NewEditor(maxDraftRunes), status: status,
		suggestionIndex: -1,
	}
}

// HandleKey updates either the approval overlay or the main composer.
func (c *Controller) HandleKey(key Key) Effect {
	if c.secret != nil {
		return c.handleSecretKey(key)
	}
	if c.approval != nil {
		return c.handleApprovalKey(key)
	}
	if c.modelPicker != nil {
		return c.handleModelPickerKey(key)
	}
	if c.configPicker != nil {
		return c.handleConfigPickerKey(key)
	}
	if c.question != nil {
		return c.handleQuestionKey(key)
	}
	if key.Kind == KeyInterrupt {
		return c.handleInterrupt()
	}
	if key.Kind == KeyEscape {
		return c.handleEscape()
	}
	c.disarmInterrupt()
	// Shift+Tab cycles the tier whether or not a completion list is open:
	// Tab completes, Shift+Tab never does, so there is nothing to disturb.
	if key.Kind == KeyShiftTab {
		return Effect{CyclePermission: true}
	}
	if effect, handled := c.handleSuggestionKey(key); handled {
		return effect
	}
	// A second request may be drafted while a turn streams. It cannot be
	// submitted concurrently into the stateful engine session, so Enter queues
	// it instead of doing nothing: the draft is taken, acknowledged on the
	// activity row, and started when the running turn finishes. Swallowing the
	// key silently was indistinguishable from a frozen terminal.
	if c.busy && key.Kind == KeyEnter {
		draft := strings.TrimSpace(c.editor.Draft())
		if draft == "" {
			return Effect{}
		}
		replacing := strings.TrimSpace(c.queued) != ""
		c.queued = c.editor.Draft()
		c.editor.clearDraft()
		c.screen.SetDraft("")
		c.clearSuggestions()
		c.syncQueued()
		c.screen.SetActivity(queuedNotice)
		if replacing {
			c.screen.SetActivity(queuedReplacedNotice)
		}
		return Effect{}
	}
	result := c.editor.Update(key)
	if result.Changed {
		c.screen.SetDraft(c.editor.Draft())
		c.updateSuggestions()
	}
	effect := Effect{Interrupt: result.Interrupt && c.busy, Exit: result.Exit}
	if result.Submit {
		if strings.HasPrefix(result.Submitted, "/") && c.commandHistory != nil {
			c.commandHistory.Record(result.Submitted)
		}
		c.clearSuggestions()
		c.busy = true
		c.setLifecycle("working")
		effect.Submit = result.Submitted
	}
	return effect
}

// handleEscape is the universal way out, one meaning per context. It runs only
// with the overlays closed — an open approval or question sees the key first —
// so the meanings it carries are its own: collapse a completion list, stop a
// running turn, and otherwise stand down anything armed. A key that does
// nothing gives no feedback at all.
func (c *Controller) handleEscape() Effect {
	switch {
	case len(c.suggestions) > 0:
		c.clearSuggestions()
		return Effect{}
	case c.busy:
		// Same meaning as Ctrl+C on a running turn, so the same handler runs:
		// the queue is dropped, the draft the user is typing survives.
		return c.handleInterrupt()
	default:
		// An armed Ctrl+C exit is waiting state on screen; Esc is the polite
		// way to have it put away without pressing anything irreversible.
		c.disarmInterrupt()
		return Effect{}
	}
}

func (c *Controller) handleInterrupt() Effect {
	// A turn running is the thing Ctrl+C most urgently means: stop it, keep
	// the app. The queued request — never yet sent — is dropped with it: Ctrl+C
	// means stop, not stop-then-start-something. The draft the user is typing
	// stays; destroying it was the old contract's worst habit. The two-stage
	// exit applies only when idle — quitting while a run streams was the only
	// way to stop that run, and it took the whole session with it.
	if c.busy {
		if c.queued != "" {
			c.queued = ""
			c.syncQueued()
		}
		return Effect{Interrupt: true}
	}
	if c.interruptArmed {
		return Effect{Exit: true}
	}
	c.interruptArmed = true
	c.editor.clearDraft()
	c.screen.SetDraft("")
	c.clearSuggestions()
	c.screen.SetActivity(interruptExitNotice)
	return Effect{}
}

// handleEscape resolves the one thing on screen a person most plausibly
// wanted to get out of: a completion menu, then a busy turn. The key was
// decoded everywhere and bound nowhere outside the question picker, so
// pressing it gave no feedback of any kind.

// disarmInterrupt returns the armed exit to safe when any other key arrives.
func (c *Controller) disarmInterrupt() {
	if !c.interruptArmed {
		return
	}
	c.interruptArmed = false
	if c.screen.activity == interruptExitNotice {
		c.screen.SetActivity("")
	}
}

// SetCommands installs the CLI-owned command catalog used for discovery.
func (c *Controller) SetCommands(commands []CommandSpec, recentLimit int) {
	c.commands = append(c.commands[:0], commands...)
	c.suggestionLimit = recentLimit
	if c.commandHistory == nil {
		c.commandHistory = NewCommandHistory(recentLimit)
	}
	c.updateSuggestions()
}

// SetFiles installs the project's files for `@` mention completion.
func (c *Controller) SetFiles(files []string) {
	c.files = append(c.files[:0], files...)
	c.updateSuggestions()
}

// SetSettings installs the settings list used for live /config filtering.
func (c *Controller) SetSettings(settings []SettingSpec) {
	c.settings = settings
	c.updateSuggestions()
}

// SetModels installs the provider model catalog used for live /model filtering.
func (c *Controller) SetModels(models []ModelSpec) {
	c.models = append(c.models[:0], models...)
	c.updateSuggestions()
}

// SetPlans installs the provider-plan catalog used for live /plogin filtering.
func (c *Controller) SetPlans(plans []PlanSpec) {
	c.plans = append(c.plans[:0], plans...)
	c.updateSuggestions()
}

// RememberCommand seeds process-local recency without executing a command.
func (c *Controller) RememberCommand(line string) {
	if c.commandHistory == nil {
		c.commandHistory = NewCommandHistory(c.suggestionLimit)
	}
	c.commandHistory.Record(line)
	c.updateSuggestions()
}

// AppendTranscript adds streamed model/tool output above the composer.
func (c *Controller) AppendTranscript(chunk string) { c.screen.AppendTranscript(chunk) }

// CommitOverflow hands back the transcript that has scrolled out of the frame,
// already styled, for the caller to put into the terminal's scrollback.
func (c *Controller) CommitOverflow(width, height int) []string {
	rows := c.screen.CommitOverflow(width, height)
	if len(rows) == 0 {
		return nil
	}
	return strings.Split(joinViewRowsWidth(rows, true, width), "\n")
}

// SetActivity updates only the ephemeral working row and its lifecycle label.
func (c *Controller) SetActivity(activity string) {
	c.screen.SetActivity(activity)
	if activity == "" {
		return
	}
	c.setLifecycle(activityPhase(activity))
}

// SetStatus replaces the session metadata without touching transcript or
// draft state. The queued count is controller-owned, not CLI-owned: the CLI
// rebuilds its Status from the engine and cannot know about the queue, so it
// is re-derived here rather than read out of the fresh value.
func (c *Controller) SetStatus(status Status) {
	status.Queued = 0
	if strings.TrimSpace(c.queued) != "" {
		status.Queued = 1
	}
	status.Agents = c.runningAgentCount()
	c.status = status
	c.screen.SetStatus(status)
}

// FinishTurn makes the editor ready without altering a type-ahead draft.
// queuedNotice tells the user the key was received and what will happen. A
// queued request that looks identical to a dropped one is a bug report.
const queuedNotice = "queued — sends when this turn finishes"

// queuedReplacedNotice is what a second Enter against an occupied queue says.
// An overwrite nobody mentions is a lost message nobody can trace.
const queuedReplacedNotice = "replaced the earlier queued request — sends when this turn finishes"

// interruptedNotice is committed to the transcript when a turn is cancelled.
// A stop has to be visible where the output it stopped is.
const interruptedNotice = "  · interrupted\n"

// BeginTurn puts the controller into the same state a submitted key does. The
// runtime uses it when it starts a queued request, so a queued turn is
// indistinguishable from a typed one: busy, working, no stale activity row.
func (c *Controller) BeginTurn() {
	c.clearAgentStatuses()
	c.busy = true
	c.screen.SetActivity("")
	c.setLifecycle("working")
}

// TakeQueued returns and clears any request queued during the last turn.
func (c *Controller) TakeQueued() string {
	queued := c.queued
	c.queued = ""
	c.syncQueued()
	return queued
}

// Queued reports the request waiting to be sent, if any.
func (c *Controller) Queued() string { return c.queued }

// syncQueued puts the held-request count on the status row, where it survives
// for the whole minute the spinner will otherwise cover the notice. A queue
// only its author remembers is a dropped message as far as the session reads.
func (c *Controller) syncQueued() {
	c.status.Queued = 0
	if strings.TrimSpace(c.queued) != "" {
		c.status.Queued = 1
	}
	c.screen.SetStatus(c.status)
}

func (c *Controller) FinishTurn(lifecycle string) {
	c.clearAgentStatuses()
	c.busy = false
	c.screen.SetActivity("")
	c.setLifecycle(lifecycle)
}

// RequestApproval displays a permission overlay with its own input buffer.
func (c *Controller) RequestApproval(approval Approval) {
	copyOfApproval := approval
	copyOfApproval.Input = ""
	c.approval = &copyOfApproval
	c.approvalEditor = NewEditor(8)
	c.beforeApproval = c.status.Lifecycle
	c.setLifecycle("approval")
}

// Approval returns a defensive copy of the current overlay, or nil.
func (c *Controller) Approval() *Approval {
	if c.approval == nil {
		return nil
	}
	copyOfApproval := *c.approval
	return &copyOfApproval
}

// Snapshot returns the independent screen regions.
func (c *Controller) Snapshot() Snapshot { return c.screen.Snapshot() }

// View renders the underlying screen without terminal styling. Golden tests
// and non-terminal adapters use this stable text representation.
func (c *Controller) View(width, height int) string {
	return c.renderView(width, height, false)
}

// RenderView adds the purple terminal palette to UI chrome while preserving
// the exact visible text returned by View.
func (c *Controller) RenderView(width, height int) string {
	return c.renderView(width, height, true)
}

func (c *Controller) renderView(width, height int, styled bool) string {
	if c.secret != nil {
		return c.overlayView(c.secretLines(width), width, height, styled)
	}
	if c.modelPicker != nil {
		return c.overlayView(c.modelPickerLines(width), width, height, styled)
	}
	if c.configPicker != nil {
		return c.overlayView(c.configPickerLines(width), width, height, styled)
	}
	if c.question != nil {
		return c.overlayView(c.questionLines(width), width, height, styled)
	}
	if c.approval == nil {
		if styled {
			return c.screen.renderView(width, height, c.editor.Cursor())
		}
		return c.screen.view(width, height, c.editor.Cursor())
	}
	return c.overlayView(c.approvalLines(width), width, height, styled)
}

// overlayView draws an overlay under the screen, shortening the screen by
// exactly the rows the overlay takes so the two never fight for the same line.
func (c *Controller) overlayView(overlay []string, width, height int, styled bool) string {
	baseHeight := height
	if height > 0 {
		baseHeight = max(0, height-len(overlay))
	}
	base := c.screen.view(width, baseHeight, c.editor.Cursor())
	if styled {
		base = c.screen.renderView(width, baseHeight, c.editor.Cursor())
		rows := make([]viewRow, len(overlay))
		for index, line := range overlay {
			style := styleNone
			if index == 0 || index == len(overlay)-1 {
				style = stylePurple
			} else if index == 1 || index == len(overlay)-2 {
				style = stylePurpleMuted
			}
			// The highlighted row carries the same marker and colour the
			// command and model pickers use, so one habit works everywhere.
			if strings.HasPrefix(line, "> ") {
				style = stylePurple
			}
			rows[index] = viewRow{text: line, style: style}
		}
		overlay = strings.Split(joinViewRows(rows, true), "\n")
	}
	if base == "" {
		return strings.Join(overlay, "\n")
	}
	return base + "\n" + strings.Join(overlay, "\n")
}

func (c *Controller) approvalLines(width int) []string {
	prompt := "Allow? [y/N]: %s▌"
	if c.approval.Rule != "" {
		prompt = "Allow? [y/N/a (" + c.approval.Rule + ")]: %s▌"
	}
	lines := []string{
		horizontalRule("approval", width),
		clipLine(sanitizeTerminalLine(c.approval.Action), width),
	}
	lines = append(lines, detailRows(c.approval.Detail, width)...)
	return append(lines,
		clipLine(sanitizeTerminalLine(fmt.Sprintf(prompt, c.approval.Input)), width),
		strings.Repeat("─", max(0, width)),
	)
}

// maxDetailRows bounds the detail an overlay shows. An overlay taller than the
// terminal pushes its own question off the screen, which is how a person ends
// up answering a prompt they cannot see.
const maxDetailRows = 32

// detailRows renders a multi-line detail as one row per line.
//
// A diff flattened onto a single row keeps every substring and destroys the
// only thing that made it readable. Each line is sanitised and clipped on its
// own, so an escape sequence in file contents cannot reach the terminal and a
// long line cannot push the layout sideways.
func detailRows(detail string, width int) []string {
	if strings.TrimSpace(detail) == "" {
		return nil
	}
	raw := strings.Split(strings.TrimSuffix(detail, "\n"), "\n")
	if len(raw) > maxDetailRows {
		head := maxDetailRows / 2
		tail := maxDetailRows - head - 1
		hidden := len(raw) - head - tail
		kept := append([]string{}, raw[:head]...)
		kept = append(kept, fmt.Sprintf("… %d more lines …", hidden))
		raw = append(kept, raw[len(raw)-tail:]...)
	}
	rows := make([]string, 0, len(raw))
	for _, line := range raw {
		rows = append(rows, clipLine(sanitizeTerminalLine(line), width))
	}
	return rows
}

func (c *Controller) handleApprovalKey(key Key) Effect {
	switch key.Kind {
	case KeyInterrupt:
		return c.resolveApproval(DecisionDeny, true, false)
	case KeyEOF:
		return c.resolveApproval(DecisionDeny, false, true)
	case KeyEscape:
		// Esc is the safe key here: it refuses and returns to the composer
		// without touching the running turn. Refusing must never require a
		// modifier the reader has to think about.
		return c.resolveApproval(DecisionDeny, false, false)
	case KeyEnter:
		answer := strings.ToLower(strings.TrimSpace(c.approvalEditor.Draft()))
		decision := DecisionDeny
		switch answer {
		case "y", "yes":
			decision = DecisionAllow
		case "a", "always":
			// Only when there is a rule to keep. Treating it as a plain yes
			// would grant something the person typing `a` did not ask for,
			// and treating it as a refusal is the safe reading.
			if c.approval.Rule != "" {
				decision = DecisionAllowAlways
			}
		}
		return c.resolveApproval(decision, false, false)
	case KeyText:
		// One keypress answers: an approval is a decision, not a line of
		// text to walk over and Enter. Anything longer still goes through
		// the editor and Enter for the person who wants to read before
		// committing.
		switch strings.ToLower(key.Text) {
		case "y":
			return c.resolveApproval(DecisionAllow, false, false)
		case "n":
			return c.resolveApproval(DecisionDeny, false, false)
		case "a":
			if c.approval.Rule != "" {
				return c.resolveApproval(DecisionAllowAlways, false, false)
			}
		}
		result := c.approvalEditor.Update(key)
		if result.Changed {
			c.approval.Input = c.approvalEditor.Draft()
		}
		return Effect{}
	default:
		result := c.approvalEditor.Update(key)
		if result.Changed {
			c.approval.Input = c.approvalEditor.Draft()
		}
		return Effect{}
	}
}

func (c *Controller) resolveApproval(decision Decision, interrupt, exit bool) Effect {
	c.approval = nil
	c.approvalEditor = nil
	c.setLifecycle(c.beforeApproval)
	c.beforeApproval = ""
	return Effect{Decision: decision, Interrupt: interrupt, Exit: exit}
}

// SetAgents updates only the running-subagent count, the way SetApproval
// updates only the tier: one field, an immediate redraw, and no disturbance to
// the transcript or a draft somebody is typing while their run works.
func (c *Controller) SetAgents(running int) {
	c.status.Agents = running
	c.screen.SetStatus(c.status)
}

// SetAgentStatus inserts or replaces one lifecycle row. IDs are the stable
// correlation key; rows are sorted by plan ordinal so concurrent starts never
// make the display jump into goroutine completion order.
func (c *Controller) SetAgentStatus(status AgentStatus) {
	key := status.ID
	if key == "" {
		key = fmt.Sprintf("agent-%d", status.Index)
	}
	if c.agentStatuses == nil {
		c.agentStatuses = map[string]AgentStatus{}
	}
	if current, found := c.agentStatuses[key]; found && status.Sequence != 0 && status.Sequence <= current.Sequence {
		return
	}
	c.agentStatuses[key] = status
	c.syncAgentStatuses()
}

func (c *Controller) syncAgentStatuses() {
	statuses := make([]AgentStatus, 0, len(c.agentStatuses))
	for _, status := range c.agentStatuses {
		statuses = append(statuses, status)
	}
	sort.SliceStable(statuses, func(i, j int) bool {
		if statuses[i].Index == statuses[j].Index {
			return statuses[i].ID < statuses[j].ID
		}
		return statuses[i].Index < statuses[j].Index
	})
	c.status.Agents = runningAgentStatuses(statuses)
	c.screen.SetAgentStatuses(statuses)
	c.screen.SetStatus(c.status)
}

func (c *Controller) clearAgentStatuses() {
	c.agentStatuses = nil
	c.status.Agents = 0
	c.screen.SetAgentStatuses(nil)
}

func (c *Controller) runningAgentCount() int {
	count := 0
	for _, status := range c.agentStatuses {
		if status.State == "working" {
			count++
		}
	}
	return count
}

func runningAgentStatuses(statuses []AgentStatus) int {
	count := 0
	for _, status := range statuses {
		if status.State == "working" {
			count++
		}
	}
	return count
}

// SetUsage updates only the footer's context and cost cells. The CLI could
// otherwise rebuild the whole status only between turns, so both numbers sat
// frozen for as long as a turn ran — which is when the context one moves.
func (c *Controller) SetUsage(context, cost string) {
	c.status.Context = context
	c.status.Cost = cost
	c.screen.SetStatus(c.status)
}

// SetApproval updates only the permission tier shown in the footer.
func (c *Controller) SetApproval(approval string) {
	c.status.Approval = approval
	c.screen.SetStatus(c.status)
}

func (c *Controller) setLifecycle(lifecycle string) {
	c.status.Lifecycle = lifecycle
	c.screen.SetStatus(c.status)
}

func (c *Controller) updateSuggestions() {
	// Mentions are checked before commands: a draft containing an `@` is
	// someone naming a file, whatever else is on the line.
	if files := SuggestFiles(c.files, c.editor.Draft(), c.suggestionLimit); len(files) > 0 {
		c.suggestions = files
		c.suggestionIndex = -1
		c.suggestionTop = 0
		c.screen.SetSuggestions(c.suggestions)
		return
	}
	if len(c.commands) == 0 {
		c.clearSuggestions()
		return
	}
	var recent []string
	if c.commandHistory != nil {
		recent = c.commandHistory.Recent()
	}
	c.suggestions = SuggestSettings(c.settings, c.editor.Draft(), c.suggestionLimit)
	if len(c.suggestions) == 0 {
		c.suggestions = SuggestModels(c.models, c.editor.Draft(), c.suggestionLimit)
	}
	if len(c.suggestions) == 0 {
		c.suggestions = SuggestPlanLogins(c.plans, c.editor.Draft(), c.suggestionLimit)
	}
	if len(c.suggestions) == 0 {
		// Every match, not the first few: the window below decides how many are on
		// screen, and a list that silently stopped at eight made the other
		// twenty-seven commands unreachable by scrolling.
		c.suggestions = SuggestCommands(c.commands, c.editor.Draft(), recent, len(c.commands))
	}
	c.suggestionIndex = -1
	c.suggestionTop = 0
	c.screen.SetSuggestions(c.suggestions)
	// The window applies from the first frame, not from the first arrow key.
	// Without this the list rendered unbounded until someone scrolled, so the
	// menu opened at whatever height the terminal allowed and then snapped to
	// eight rows the moment a key was pressed.
	c.screen.SetSuggestionWindow(0, c.windowSize(), len(c.suggestions))
}

func (c *Controller) handleSuggestionKey(key Key) (Effect, bool) {
	if len(c.suggestions) == 0 {
		return Effect{}, false
	}
	switch key.Kind {
	case KeyDown:
		c.suggestionIndex = (c.suggestionIndex + 1) % len(c.suggestions)
		c.showSelectedSuggestion()
		return Effect{}, true
	case KeyUp:
		if c.suggestionIndex <= 0 {
			c.suggestionIndex = len(c.suggestions) - 1
		} else {
			c.suggestionIndex--
		}
		c.showSelectedSuggestion()
		return Effect{}, true
	case KeyPageDown:
		c.suggestionIndex = min(len(c.suggestions)-1, max(0, c.suggestionIndex)+c.suggestionLimit)
		c.showSelectedSuggestion()
		return Effect{}, true
	case KeyPageUp:
		c.suggestionIndex = max(0, max(0, c.suggestionIndex)-c.suggestionLimit)
		c.showSelectedSuggestion()
		return Effect{}, true
	case KeyTab:
		if c.suggestionIndex < 0 {
			c.suggestionIndex = 0
		}
		c.completeSuggestion(c.suggestions[c.suggestionIndex])
		return Effect{}, true
	case KeyEnter:
		if c.suggestionIndex >= 0 {
			c.completeSuggestion(c.suggestions[c.suggestionIndex])
			return Effect{}, true
		}
	}
	return Effect{}, false
}

// windowSize is how many suggestion rows are on screen at once.
func (c *Controller) windowSize() int {
	if c.suggestionLimit > 0 {
		return c.suggestionLimit
	}
	return 8
}

// showSelectedSuggestion scrolls the window the least amount that puts the
// selection on screen, so holding an arrow key walks the list one row at a
// time instead of paging under the cursor.
func (c *Controller) showSelectedSuggestion() {
	window := c.windowSize()
	c.suggestionTop = scrollWindow(c.suggestionIndex, c.suggestionTop, window)
	c.screen.SetSuggestionWindow(c.suggestionTop, window, len(c.suggestions))
	c.screen.SetSuggestionSelection(c.suggestionIndex)
}

func (c *Controller) completeSuggestion(command CommandSpec) {
	completion := command.Complete
	if completion == "" {
		completion = "/" + command.Name
	}
	if strings.TrimSpace(command.Usage) != completion {
		completion += " "
	}
	c.editor.setDraft(completion)
	c.screen.SetDraft(completion)
	c.clearSuggestions()
}

func (c *Controller) clearSuggestions() {
	c.suggestions = nil
	c.suggestionIndex = -1
	c.screen.SetSuggestions(nil)
}

func activityPhase(activity string) string {
	for _, phase := range []string{"thinking", "planning", "working", "synthesizing", "streaming"} {
		if strings.Contains(activity, phase) {
			return phase
		}
	}
	return "working"
}

// SecretPrompt is the masked overlay: a credential is typed here, rendered as
// dots, delivered once on Enter, and kept nowhere. Typed is the only thing the
// view learns about the draft.
type SecretPrompt struct {
	Prompt string
	Typed  int
}

// RequestSecret opens the masked overlay. Like the approval overlay it has
// its own editor, so the main draft is untouched, and it is wide enough for
// any key a keystore accepts.
func (c *Controller) RequestSecret(prompt string) {
	c.secret = &SecretPrompt{Prompt: prompt}
	c.secretEditor = NewEditor(1024)
	c.beforeSecret = c.status.Lifecycle
	c.setLifecycle("key")
}

// Secret returns a copy of the open masked overlay, or nil.
func (c *Controller) Secret() *SecretPrompt {
	if c.secret == nil {
		return nil
	}
	copyOfSecret := *c.secret
	return &copyOfSecret
}

func (c *Controller) handleSecretKey(key Key) Effect {
	switch key.Kind {
	case KeyInterrupt:
		return c.resolveSecret("", false, true)
	case KeyEOF, KeyEscape:
		return c.resolveSecret("", false, false)
	case KeyEnter:
		return c.resolveSecret(c.secretEditor.Draft(), true, false)
	default:
		if result := c.secretEditor.Update(key); result.Changed {
			c.secret.Typed = len([]rune(c.secretEditor.Draft()))
		}
		return Effect{}
	}
}

func (c *Controller) resolveSecret(value string, submitted, interrupt bool) Effect {
	c.secret = nil
	c.secretEditor = nil
	c.setLifecycle(c.beforeSecret)
	c.beforeSecret = ""
	return Effect{Secret: value, SecretSubmitted: submitted, SecretDismissed: !submitted, Interrupt: interrupt}
}

func (c *Controller) secretLines(width int) []string {
	return []string{
		horizontalRule("key", width),
		clipLine(sanitizeTerminalLine(c.secret.Prompt), width),
		clipLine(strings.Repeat("•", c.secret.Typed)+"▌", width),
		clipLine("Enter saves · Esc cancels · nothing typed here is shown, kept or recalled", width),
		strings.Repeat("─", max(0, width)),
	}
}
