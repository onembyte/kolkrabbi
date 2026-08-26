// Package engine ties the API client, tools, session persistence, file
// checkpoints and local stats together, and implements the release modes:
//
//	chat  — plain conversation, no tools
//	code  — the classic agentic coding loop (tools until a final answer)
package engine

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"
	"unicode"

	"github.com/onembyte/kolkrabbi/internal/bus"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/tools"
	"github.com/onembyte/kolkrabbi/internal/xid"
	"github.com/onembyte/kolkrabbi/protocol"
)

const (
	colorReset = "\033[0m"
	colorDim   = "\033[2m"
	colorCyan  = "\033[36m"
	colorYel   = "\033[33m"
	colorMag   = "\033[35m"
)

// Modes and efforts.
const (
	ModeChat  = "chat"
	ModeCode  = "code"
	ModeAgent = "agent"
)

var Modes = []string{ModeChat, ModeCode, ModeAgent}

const (
	EffortLow    = "low"
	EffortMedium = "medium"
	EffortHigh   = "high"
	EffortMax    = "max"

	// Legacy aliases preserved for backward compatibility
	EffortQuick    = EffortLow
	EffortStandard = EffortMedium
	EffortDeep     = EffortHigh
	EffortUltra    = EffortMax
)

var CanonicalEfforts = []string{EffortLow, EffortMedium, EffortHigh, EffortMax}

var Efforts = []string{EffortLow, EffortMedium, EffortHigh, EffortMax, "quick", "standard", "deep", "ultra"}

// NormalizeEffort maps any valid effort name, alias, or numeric string (1..4)
// to its canonical value ("low", "medium", "high", "max"). It returns false
// if the input is not recognized.
func NormalizeEffort(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "low", "l", "1", "quick":
		return EffortLow, true
	case "medium", "med", "m", "2", "standard":
		return EffortMedium, true
	case "high", "h", "3", "deep":
		return EffortHigh, true
	case "max", "x", "4", "ultra":
		return EffortMax, true
	default:
		return "", false
	}
}

// MaxRoundsFor returns the maximum tool rounds permitted for a single turn,
// governed by mode and active effort level.
func MaxRoundsFor(mode string, effort string) int {
	eff, ok := NormalizeEffort(effort)
	if !ok {
		eff = EffortMedium
	}
	if mode == ModeChat {
		switch eff {
		case EffortLow:
			return 2
		case EffortHigh:
			return 12
		case EffortMax:
			return 20
		default: // EffortMedium
			return 6
		}
	}
	// ModeCode / ModeAgent
	switch eff {
	case EffortLow:
		return 4
	case EffortHigh:
		return 24
	case EffortMax:
		return 50
	default: // EffortMedium
		return 12
	}
}

// TimeoutForEffort returns the bash command timeout for a given effort level.
func TimeoutForEffort(effort string) time.Duration {
	eff, ok := NormalizeEffort(effort)
	if !ok {
		eff = EffortMedium
	}
	switch eff {
	case EffortLow:
		return 30 * time.Second
	case EffortHigh:
		return 300 * time.Second
	case EffortMax:
		return 600 * time.Second
	default: // EffortMedium
		return 120 * time.Second
	}
}

// projectMemoryFiles are looked up in the working directory, first match
// wins; their content is appended to the system prompt (like CLAUDE.md).
var projectMemoryFiles = []string{"KOLKRABBI.md", "AGENTS.md"}

const maxProjectMemory = 16 * 1024

const emptyCompletionRecovery = "The previous response was empty. Continue the original user request now. Use tools when needed, and finish the requested concrete step or explain a specific blocker."

const (
	activityThinking     = "thinking"
	activityPlanning     = "planning"
	activityWorking      = "working"
	activitySynthesizing = "synthesizing"
)

// ActivityIndicator observes the lifetime of one logical provider call. Start
// must return promptly; its stop function may join any indicator-owned work so
// the engine can guarantee that presentation is gone before output continues.
type ActivityIndicator interface {
	Start(context.Context, string) func()
}

// WorkIndicator presents one local tool action independently from provider
// waiting. Surfaces may render it ephemerally; the engine still writes one
// durable, human-readable activity line to Out.
type WorkIndicator interface {
	StartWork(context.Context, string) func()
}

// Options configures an Agent; zero values get sensible defaults.
type Options struct {
	Client   *provider.Client
	Model    string // the session's base model
	Mode     string // chat | code | agent (default code)
	Effort   string // quick | standard | deep | ultra (default standard)
	Yolo     bool
	Sess     SessionPort
	Ckpt     Checkpointer  // may be nil (checkpointing disabled)
	In       *bufio.Reader // shared stdin reader; may be nil with Yolo
	Out      io.Writer     // defaults to os.Stdout
	Recorder Recorder      // records stats/ratings; nil disables stats
	Clock    Clock         // nil defaults to time.Now
	// RetryWait is the cancellable wait used between bounded provider retries.
	// Nil selects the real timer; tests inject it to keep retry gates instant.
	RetryWait func(context.Context, time.Duration) error
	Activity  ActivityIndicator
	Work      WorkIndicator
	Decider   Decider
	// Tiers maps effort level -> model id. Missing tiers fall back to Model,
	// so everything works zero-config and tiers are a pure optimization.
	Tiers       map[string]string
	Bus         *bus.Bus
	PinnedModel bool
	FreeModels  []string
}

type Agent struct {
	Options
	lastTurnID string
	saveWarned bool
	statsWarn  bool
}

// New wires up an agent around an existing (possibly resumed) session. The
// session's system prompt is (re)generated so cwd and project memory are
// always current, and any dangling tool calls from an interrupted run are
// repaired so the next API call is valid.
func New(o Options) *Agent {
	if o.Out == nil {
		o.Out = os.Stdout
	}
	if o.Mode == "" {
		o.Mode = ModeCode
	}
	if o.Effort == "" {
		o.Effort = EffortMedium
	} else if canonical, ok := NormalizeEffort(o.Effort); ok {
		o.Effort = canonical
	}
	if o.Tiers == nil {
		o.Tiers = map[string]string{}
	}
	if o.RetryWait == nil {
		o.RetryWait = waitForRetry
	}
	if o.Decider == nil && o.In != nil {
		o.Decider = NewTerminalDecider(o.In, o.Out)
	}
	a := &Agent{Options: o}

	if o.Sess != nil {
		sys := provider.Message{Role: "system", Content: a.systemPrompt(o.Mode)}
		msgs := o.Sess.GetMessages()
		if len(msgs) == 0 {
			o.Sess.AppendMessage(sys)
		} else {
			msgs[0] = sys
			o.Sess.SetMessages(msgs)
		}
		a.repairDanglingToolCalls()
		if o.Sess.ModelName() == "" {
			o.Sess.SetModelName(o.Model)
		}
	}
	return a
}

// SetMode switches mode and refreshes the system prompt accordingly.
func (a *Agent) SetMode(mode string) error {
	for _, m := range Modes {
		if m == mode {
			a.Mode = mode
			if a.Sess != nil {
				msgs := a.Sess.GetMessages()
				if len(msgs) > 0 {
					msgs[0] = provider.Message{Role: "system", Content: a.systemPrompt(mode)}
					a.Sess.SetMessages(msgs)
				}
			}
			return nil
		}
	}
	return fmt.Errorf("unknown mode %q (chat|code|agent)", mode)
}

// SetEffort validates, normalizes and sets the effort level.
func (a *Agent) SetEffort(effort string) error {
	if canonical, ok := NormalizeEffort(effort); ok {
		a.Effort = canonical
		return nil
	}
	return fmt.Errorf("invalid effort %q: expected low (1), medium (2), high (3), or max (4)", effort)
}

// ModelForEffort resolves the model for an effort level (accepting any canonical,
// numeric, or legacy alias): configured tier if set, otherwise the session's base model.
func (a *Agent) ModelForEffort(effort string) string {
	return a.modelFor(effort)
}

// modelFor resolves the model for an effort level: configured tier if set,
// otherwise the session's base model. Zero-config always works.
func (a *Agent) modelFor(effort string) string {
	eff, ok := NormalizeEffort(effort)
	if !ok {
		eff = effort
	}
	// 1. Direct canonical tier hit
	if m, ok := a.Tiers[eff]; ok && m != "" {
		return m
	}
	// 2. Legacy tier key fallback if user config used "quick", "standard", etc.
	switch eff {
	case EffortLow:
		if m, ok := a.Tiers["quick"]; ok && m != "" {
			return m
		}
	case EffortMedium:
		if m, ok := a.Tiers["standard"]; ok && m != "" {
			return m
		}
	case EffortHigh:
		if m, ok := a.Tiers["deep"]; ok && m != "" {
			return m
		}
	case EffortMax:
		if m, ok := a.Tiers["ultra"]; ok && m != "" {
			return m
		}
	}
	return a.Model
}

// toolsFor returns the tool set for a mode: chat gets none.
func toolsFor(mode string) []provider.Tool {
	if mode == ModeChat {
		return nil
	}
	return tools.Definitions()
}

func (a *Agent) systemPrompt(mode string) string {
	cwd, _ := os.Getwd()
	var sys string
	switch mode {
	case ModeChat:
		sys = fmt.Sprintf(`You are Kolkrabbi, in chat mode: a concise, direct assistant in a terminal on %s (working directory %s). You have no tools in this mode: answer from knowledge, and if a task needs file access or shell commands, say so and suggest switching to code mode (/mode code).`, runtime.GOOS, cwd)
	default:
		sys = fmt.Sprintf(`You are Kolkrabbi, a fast, terminal-based coding agent running on %s in the directory %s.

You have tools to read/write/edit files, list directories, and run shell commands. Use them proactively to accomplish what the user asks instead of just describing what you would do. Prefer small, verifiable steps: read before you edit, run tests/builds after changing code when reasonable.

Be concise in your prose responses. Do not narrate every tool call at length; let the tool results speak for themselves and summarize only what matters.`, runtime.GOOS, cwd)
		sys += `

When asked to build or continue a project, inspect the relevant plan and checkpoint, select one concrete unfinished checkpoint, and carry it through implementation and verification. Do not stop after inspection: keep using tools until that checkpoint is complete, or state a concrete blocker and the evidence for it.`
	}

	for _, name := range projectMemoryFiles {
		b, err := os.ReadFile(name)
		if err != nil {
			continue
		}
		if len(b) > maxProjectMemory {
			b = b[:maxProjectMemory]
		}
		sys += fmt.Sprintf("\n\nProject notes (from %s):\n%s", name, string(b))
		break
	}
	return sys
}

// repairDanglingToolCalls handles a session that was interrupted between an
// assistant message with tool_calls and its tool results: the API rejects a
// history where tool calls have no results, so we append synthetic ones.
func (a *Agent) repairDanglingToolCalls() {
	if a.Sess == nil {
		return
	}
	msgs := a.Sess.GetMessages()
	last := -1
	for i, m := range msgs {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			last = i
		}
	}
	if last == -1 {
		return
	}
	answered := map[string]bool{}
	for _, m := range msgs[last+1:] {
		if m.Role != "tool" {
			return // a non-tool message follows, so this block was completed
		}
		answered[m.ToolCallID] = true
	}
	for _, tc := range msgs[last].ToolCalls {
		if !answered[tc.ID] {
			msgs = append(msgs, provider.Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    "Interrupted before this tool ran. Re-issue the call if it is still needed.",
			})
		}
	}
	a.Sess.SetMessages(msgs)
}

func (a *Agent) save() {
	if a.Sess == nil {
		return
	}
	if err := a.Sess.Save(); err != nil && !a.saveWarned {
		fmt.Fprintf(os.Stderr, "\nwarning: could not save session: %v\n", err)
		a.saveWarned = true
	}
}

// record appends a stats line; never fatal, warn once.
func (a *Agent) record(role string, meta provider.Meta, toolCalls int) {
	if a.Recorder == nil || a.Sess == nil {
		return
	}
	err := a.Recorder.RecordCall(CallRecord{
		Session:          a.Sess.SessionID(),
		Turn:             a.lastTurnID,
		Mode:             a.Mode,
		Effort:           a.Effort,
		Role:             role,
		Model:            meta.Model,
		PromptTokens:     meta.PromptTokens,
		CompletionTokens: meta.CompletionTokens,
		Cost:             meta.Cost,
		Ms:               meta.Elapsed.Milliseconds(),
		ToolCalls:        toolCalls,
	})
	if err != nil && !a.statsWarn {
		fmt.Fprintf(os.Stderr, "\nwarning: could not record stats: %v\n", err)
		a.statsWarn = true
	}
}

// RateLast attaches a 1–5 rating to the most recent turn.
func (a *Agent) RateLast(rating int) error {
	if rating < 1 || rating > 5 {
		return fmt.Errorf("rating must be 1-5")
	}
	if a.lastTurnID == "" {
		return fmt.Errorf("nothing to rate yet")
	}
	if a.Recorder == nil || a.Sess == nil {
		return fmt.Errorf("stats are disabled")
	}
	return a.Recorder.RecordRating(a.Sess.SessionID(), a.lastTurnID, rating)
}

func newTurnID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (a *Agent) confirm(ctx context.Context, action, detail string) bool {
	if a.Yolo {
		return true
	}
	if a.Decider == nil {
		return false
	}

	confirmation := Confirmation{Action: action, Detail: detail}
	permID := xid.New(xid.Call)
	turnID := a.lastTurnID
	if turnID == "" {
		turnID = xid.New(xid.Turn)
	}

	if a.Bus != nil {
		reqData, _ := json.Marshal(protocol.PermissionRequestedData{
			ID:     permID,
			Tool:   action,
			Detail: detail,
		})
		_, _ = a.Bus.Publish(bus.Event{
			Turn: turnID,
			Type: protocol.EventPermissionRequested,
			Data: reqData,
		})
	}

	var decision protocol.PermissionDecision
	if pd, ok := a.Decider.(PolicyDecider); ok {
		decision = pd.Decide(ctx, confirmation)
	} else if a.Decider.Confirm(ctx, confirmation) {
		decision = protocol.PermissionDecisionAllow
	} else {
		decision = protocol.PermissionDecisionDeny
	}

	allowed := (decision == protocol.PermissionDecisionAllow || decision == protocol.PermissionDecisionAllowSession)

	if a.Bus != nil {
		resData, _ := json.Marshal(protocol.PermissionResolvedData{
			ID:       permID,
			Decision: decision,
		})
		_, _ = a.Bus.Publish(bus.Event{
			Turn: turnID,
			Type: protocol.EventPermissionResolved,
			Data: resData,
		})
	}

	return allowed
}

func (a *Agent) confirmer(ctx context.Context) tools.Confirm {
	return func(action, detail string) bool {
		return a.confirm(ctx, action, detail)
	}
}

// preWrite is the checkpoint hook handed to tools.Execute.
func (a *Agent) preWrite(tool, path string) error {
	if a.Ckpt == nil {
		return nil
	}
	return a.Ckpt.Record(tool, path)
}

func (a *Agent) responseLabel() string { return "kolk-" + a.Mode }

func describeToolCall(tc provider.ToolCall) string {
	var args struct {
		Command     string `json:"command"`
		Description string `json:"description"`
		Path        string `json:"path"`
	}
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		return "Using tool — " + compactToolText(tc.Function.Name)
	}
	path := compactToolText(args.Path)
	switch tc.Function.Name {
	case "bash":
		detail := compactToolText(args.Description)
		if detail == "" {
			detail = compactToolText(args.Command)
		}
		if detail != "" {
			return "Running command — " + detail
		}
	case "read_file":
		if path != "" {
			return "Reading file — " + path
		}
	case "write_file":
		if path != "" {
			return "Writing file — " + path
		}
	case "edit_file":
		if path != "" {
			return "Editing file — " + path
		}
	case "list_dir":
		if path == "" {
			path = "."
		}
		return "Listing directory — " + path
	}
	return "Using tool — " + compactToolText(tc.Function.Name)
}

func compactToolText(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > 120 {
		value = string(runes[:120]) + "…"
	}
	return value
}

func (a *Agent) executeTool(ctx context.Context, tc provider.ToolCall) (string, error) {
	description := describeToolCall(tc)
	fmt.Fprintf(a.Out, "%s  → %s%s\n", colorDim, description, colorReset)
	stopWork := func() {}
	if a.Work != nil {
		if stop := a.Work.StartWork(ctx, description); stop != nil {
			stopWork = stop
		}
	}
	defer stopWork()
	toolCtx := ctx
	if tc.Function.Name == "bash" {
		var cancel context.CancelFunc
		toolCtx, cancel = context.WithTimeout(ctx, TimeoutForEffort(a.Effort))
		defer cancel()
	}
	return tools.Execute(toolCtx, tc.Function.Name, tc.Function.Arguments, a.confirmer(toolCtx), a.preWrite)
}

func (a *Agent) footer(meta provider.Meta) {
	if a.Bus != nil {
		inToks := int64(meta.PromptTokens)
		outToks := int64(meta.CompletionTokens)
		ttft := int64(meta.Elapsed.Milliseconds())
		cost := meta.Cost
		usageData, _ := json.Marshal(protocol.UsageReportedData{
			Model:            meta.Model,
			ProviderName:     "openrouter",
			RequestModel:     meta.Model,
			InputTokens:      &inToks,
			OutputTokens:     &outToks,
			CostUSD:          &cost,
			CostSource:       protocol.UsageCostReported,
			Measurement:      protocol.UsageMeasurementMetered,
			TTFTMilliseconds: &ttft,
			Attempt:          1,
			Role:             "main",
			Effort:           a.Effort,
		})
		_, _ = a.Bus.Publish(bus.Event{
			Turn: a.lastTurnID,
			Type: protocol.EventUsageReported,
			Data: usageData,
		})
	}
	cost := ""
	if meta.Cost > 0 {
		cost = fmt.Sprintf(" · $%.4f", meta.Cost)
	}
	toks := ""
	if meta.PromptTokens+meta.CompletionTokens > 0 {
		toks = fmt.Sprintf(" · %d tok", meta.PromptTokens+meta.CompletionTokens)
	}
	fmt.Fprintf(a.Out, "%s  [%s · %s%s%s · %dms]%s\n",
		colorDim, a.Mode, meta.Model, toks, cost, meta.Elapsed.Milliseconds(), colorReset)
}

// RunTurn dispatches a user message according to the current mode.
func (a *Agent) RunTurn(ctx context.Context, userInput string) error {
	a.lastTurnID = xid.New(xid.Turn)
	if a.Ckpt != nil {
		a.Ckpt.BeginTurn()
	}
	if a.Sess != nil {
		a.Sess.SetTitleFromInput(userInput)
	}

	if a.Bus != nil {
		startedData, _ := json.Marshal(protocol.TurnStartedData{
			Input:  userInput,
			Model:  a.modelFor(a.Effort),
			Mode:   a.Mode,
			Effort: a.Effort,
		})
		_, _ = a.Bus.Publish(bus.Event{
			Turn: a.lastTurnID,
			Type: protocol.EventTurnStarted,
			Data: startedData,
		})
	}

	var err error
	if a.Mode == ModeAgent {
		err = a.runOrchestrated(ctx, userInput)
	} else {
		err = a.runLoop(ctx, userInput)
	}

	if a.Bus != nil {
		if err != nil {
			if ctx.Err() != nil {
				cancelledData, _ := json.Marshal(protocol.TurnCancelledData{Reason: "cancelled"})
				_, _ = a.Bus.Publish(bus.Event{
					Turn: a.lastTurnID,
					Type: protocol.EventTurnCancelled,
					Data: cancelledData,
				})
			}
		} else {
			finishedData, _ := json.Marshal(protocol.TurnFinishedData{Reason: "stop"})
			_, _ = a.Bus.Publish(bus.Event{
				Turn: a.lastTurnID,
				Type: protocol.EventTurnFinished,
				Data: finishedData,
			})
		}
	}
	return err
}

// runLoop is the chat/code path: stream the reply, execute any tool calls,
// loop until the model returns a plain answer. The session is saved as it
// goes.
func (a *Agent) runLoop(ctx context.Context, userInput string) error {
	if a.Sess != nil {
		a.Sess.AppendMessage(provider.Message{Role: "user", Content: userInput})
		a.save()
	}

	model := a.modelFor(a.Effort)
	toolset := toolsFor(a.Mode)
	var requestMessages []provider.Message
	if a.Sess != nil {
		requestMessages = a.Sess.GetMessages()
	} else {
		requestMessages = []provider.Message{
			{Role: "system", Content: a.systemPrompt(a.Mode)},
			{Role: "user", Content: userInput},
		}
	}
	emptyCompletions := 0
	toolRounds := 0
	maxRounds := MaxRoundsFor(a.Mode, a.Effort)

	for {
		fmt.Fprintf(a.Out, "%s%s%s ", colorCyan, a.responseLabel(), colorReset)
		msg, meta, err := a.streamChat(ctx, activityThinking, model, requestMessages, toolset, func(tok string) {
			if a.Bus != nil {
				deltaData, _ := json.Marshal(protocol.MessageDeltaData{Text: tok})
				_, _ = a.Bus.Publish(bus.Event{
					Turn: a.lastTurnID,
					Type: protocol.EventMessageDelta,
					Data: deltaData,
				})
			}
			fmt.Fprint(a.Out, tok)
		})
		if err != nil {
			fmt.Fprintln(a.Out)
			return err
		}
		fmt.Fprintln(a.Out)
		if a.Bus != nil && msg.Content != "" {
			completedData, _ := json.Marshal(protocol.MessageCompletedData{Text: msg.Content})
			_, _ = a.Bus.Publish(bus.Event{
				Turn: a.lastTurnID,
				Type: protocol.EventMessageCompleted,
				Data: completedData,
			})
		}
		a.record("main", meta, len(msg.ToolCalls))
		if strings.TrimSpace(msg.Content) == "" && len(msg.ToolCalls) == 0 {
			emptyCompletions++
			if emptyCompletions >= 2 {
				return fmt.Errorf("model returned two empty responses; try `/model` to select another model")
			}
			fmt.Fprintln(a.Out, colorDim+"  (empty model response; retrying once)"+colorReset)
			if a.Sess != nil {
				requestMessages = appendMessage(a.Sess.GetMessages(), provider.Message{
					Role: "user", Content: emptyCompletionRecovery,
				})
			} else {
				requestMessages = appendMessage(requestMessages, provider.Message{
					Role: "user", Content: emptyCompletionRecovery,
				})
			}
			continue
		}

		if a.Sess != nil {
			a.Sess.AppendMessage(msg)
			a.save()
		}

		if len(msg.ToolCalls) == 0 {
			a.footer(meta)
			return nil // final answer for this turn
		}
		emptyCompletions = 0
		toolRounds++
		if toolRounds > maxRounds {
			return fmt.Errorf("exceeded maximum tool rounds (%d) for %s effort", maxRounds, a.Effort)
		}

		for _, tc := range msg.ToolCalls {
			if a.Bus != nil {
				reqData, _ := json.Marshal(protocol.ToolRequestedData{
					ID:        tc.ID,
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
					Executor:  protocol.ToolExecutorKolk,
				})
				_, _ = a.Bus.Publish(bus.Event{
					Turn: a.lastTurnID,
					Type: protocol.EventToolRequested,
					Data: reqData,
				})
			}
			result, err := a.executeTool(ctx, tc)
			if err != nil {
				result = "Error: " + err.Error()
			}
			if a.Bus != nil {
				outData, _ := json.Marshal(protocol.ToolOutputData{
					ID:       tc.ID,
					Output:   result,
					Executor: protocol.ToolExecutorKolk,
				})
				_, _ = a.Bus.Publish(bus.Event{
					Turn: a.lastTurnID,
					Type: protocol.EventToolOutput,
					Data: outData,
				})
			}
			if a.Sess != nil {
				a.Sess.AppendMessage(provider.Message{
					Role:       "tool",
					ToolCallID: tc.ID,
					Content:    result,
				})
			}
		}
		if a.Sess != nil {
			a.save()
			requestMessages = a.Sess.GetMessages()
		}
		// loop: send tool results back to the model for its next step
	}
}

func appendMessage(messages []provider.Message, message provider.Message) []provider.Message {
	copyOfMessages := make([]provider.Message, len(messages), len(messages)+1)
	copy(copyOfMessages, messages)
	return append(copyOfMessages, message)
}

// Rewind undoes the file changes of the most recent turn (files only; the
// conversation itself is untouched). Returns the restored paths.
func (a *Agent) Rewind() ([]string, error) {
	if a.Ckpt == nil {
		return nil, fmt.Errorf("checkpointing is not enabled")
	}
	return a.Ckpt.RewindLastTurn()
}
