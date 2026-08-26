package tui

import (
	"fmt"
	"strings"
)

const interruptExitNotice = "Input cleared. Press Ctrl+C again to exit."

// Decision is the result of one visible approval overlay.
type Decision uint8

const (
	DecisionNone Decision = iota
	DecisionDeny
	DecisionAllow
)

// Effect asks the runtime to perform work outside the pure controller.
type Effect struct {
	Submit    string
	Interrupt bool
	Exit      bool
	Decision  Decision
}

// Approval is the independent permission overlay. Input never shares storage
// with the main composer draft.
type Approval struct {
	Action string
	Detail string
	Input  string
}

// Controller applies terminal and engine events to one screen and editor.
type Controller struct {
	screen          *Model
	editor          *Editor
	status          Status
	busy            bool
	approval        *Approval
	approvalEditor  *Editor
	beforeApproval  string
	commands        []CommandSpec
	models          []ModelSpec
	plans           []PlanSpec
	commandHistory  *CommandHistory
	suggestionLimit int
	suggestions     []CommandSpec
	suggestionIndex int
	interruptArmed  bool
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
	if c.approval != nil {
		return c.handleApprovalKey(key)
	}
	if key.Kind == KeyInterrupt {
		return c.handleInterrupt()
	}
	c.disarmInterrupt()
	if effect, handled := c.handleSuggestionKey(key); handled {
		return effect
	}
	// A second request may be drafted while a turn streams, but it is not
	// submitted concurrently into the stateful engine session.
	if c.busy && key.Kind == KeyEnter {
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

func (c *Controller) handleInterrupt() Effect {
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

// SetActivity updates only the ephemeral working row and its lifecycle label.
func (c *Controller) SetActivity(activity string) {
	c.screen.SetActivity(activity)
	if activity == "" {
		return
	}
	c.setLifecycle(activityPhase(activity))
}

// SetStatus replaces the session metadata without touching transcript or
// draft state.
func (c *Controller) SetStatus(status Status) {
	c.status = status
	c.screen.SetStatus(status)
}

// FinishTurn makes the editor ready without altering a type-ahead draft.
func (c *Controller) FinishTurn(lifecycle string) {
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
	if c.approval == nil {
		if styled {
			return c.screen.renderView(width, height, c.editor.Cursor())
		}
		return c.screen.view(width, height, c.editor.Cursor())
	}
	overlay := c.approvalLines(width)
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
	return []string{
		horizontalRule("approval", width),
		clipLine(sanitizeTerminalLine(c.approval.Action), width),
		clipLine(sanitizeTerminalLine(c.approval.Detail), width),
		clipLine(sanitizeTerminalLine(fmt.Sprintf("Allow? [y/N]: %s▌", c.approval.Input)), width),
		strings.Repeat("─", max(0, width)),
	}
}

func (c *Controller) handleApprovalKey(key Key) Effect {
	switch key.Kind {
	case KeyInterrupt:
		return c.resolveApproval(DecisionDeny, true, false)
	case KeyEOF:
		return c.resolveApproval(DecisionDeny, false, true)
	case KeyEnter:
		answer := strings.ToLower(strings.TrimSpace(c.approvalEditor.Draft()))
		decision := DecisionDeny
		if answer == "y" || answer == "yes" {
			decision = DecisionAllow
		}
		return c.resolveApproval(decision, false, false)
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

func (c *Controller) setLifecycle(lifecycle string) {
	c.status.Lifecycle = lifecycle
	c.screen.SetStatus(c.status)
}

func (c *Controller) updateSuggestions() {
	if len(c.commands) == 0 {
		c.clearSuggestions()
		return
	}
	var recent []string
	if c.commandHistory != nil {
		recent = c.commandHistory.Recent()
	}
	c.suggestions = SuggestModels(c.models, c.editor.Draft(), c.suggestionLimit)
	if len(c.suggestions) == 0 {
		c.suggestions = SuggestPlanLogins(c.plans, c.editor.Draft(), c.suggestionLimit)
	}
	if len(c.suggestions) == 0 {
		c.suggestions = SuggestCommands(c.commands, c.editor.Draft(), recent, c.suggestionLimit)
	}
	c.suggestionIndex = -1
	c.screen.SetSuggestions(c.suggestions)
}

func (c *Controller) handleSuggestionKey(key Key) (Effect, bool) {
	if len(c.suggestions) == 0 {
		return Effect{}, false
	}
	switch key.Kind {
	case KeyDown:
		c.suggestionIndex = (c.suggestionIndex + 1) % len(c.suggestions)
		c.screen.SetSuggestionSelection(c.suggestionIndex)
		return Effect{}, true
	case KeyUp:
		if c.suggestionIndex <= 0 {
			c.suggestionIndex = len(c.suggestions) - 1
		} else {
			c.suggestionIndex--
		}
		c.screen.SetSuggestionSelection(c.suggestionIndex)
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
