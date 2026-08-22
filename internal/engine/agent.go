// Package agent ties the API client, tools, session persistence, file
// checkpoints and local stats together, and implements the three modes:
//
//	chat  — plain conversation, no tools
//	code  — the classic agentic coding loop (tools until a final answer)
//	agent — orchestrated: plan → delegate to isolated subagents → synthesize
package engine

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/checkpoint"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/session"
	"github.com/onembyte/kolkrabbi/internal/stats"
	"github.com/onembyte/kolkrabbi/internal/tools"
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
	EffortQuick    = "quick"
	EffortStandard = "standard"
	EffortDeep     = "deep"
	EffortUltra    = "ultra"
)

var Efforts = []string{EffortQuick, EffortStandard, EffortDeep, EffortUltra}

// projectMemoryFiles are looked up in the working directory, first match
// wins; their content is appended to the system prompt (like CLAUDE.md).
var projectMemoryFiles = []string{"KOLKRABBI.md", "AGENTS.md"}

const maxProjectMemory = 16 * 1024

// Options configures an Agent; zero values get sensible defaults.
type Options struct {
	Client   *provider.Client
	Model    string // the session's base model
	Mode     string // chat | code | agent (default code)
	Effort   string // quick | standard | deep | ultra (default standard)
	Yolo     bool
	Sess     *session.Session
	Ckpt     *checkpoint.Store // may be nil (checkpointing disabled)
	In       *bufio.Reader     // shared stdin reader; may be nil with Yolo
	Out      io.Writer         // defaults to os.Stdout
	StatsDir string            // where stats.jsonl lives; "" disables stats
	// Tiers maps effort level -> model id. Missing tiers fall back to Model,
	// so everything works zero-config and tiers are a pure optimization.
	Tiers map[string]string
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
		o.Effort = EffortStandard
	}
	if o.Tiers == nil {
		o.Tiers = map[string]string{}
	}
	a := &Agent{Options: o}

	sys := provider.Message{Role: "system", Content: a.systemPrompt(o.Mode)}
	if len(o.Sess.Messages) == 0 {
		o.Sess.Messages = append(o.Sess.Messages, sys)
	} else {
		o.Sess.Messages[0] = sys
	}
	a.repairDanglingToolCalls()
	if o.Sess.Model == "" {
		o.Sess.Model = o.Model
	}
	return a
}

// SetMode switches mode and refreshes the system prompt accordingly.
func (a *Agent) SetMode(mode string) error {
	for _, m := range Modes {
		if m == mode {
			a.Mode = mode
			a.Sess.Messages[0] = provider.Message{Role: "system", Content: a.systemPrompt(mode)}
			return nil
		}
	}
	return fmt.Errorf("unknown mode %q (chat|code|agent)", mode)
}

// SetEffort validates and sets the effort level.
func (a *Agent) SetEffort(effort string) error {
	for _, e := range Efforts {
		if e == effort {
			a.Effort = effort
			return nil
		}
	}
	return fmt.Errorf("unknown effort %q (quick|standard|deep|ultra)", effort)
}

// modelFor resolves the model for an effort level: configured tier if set,
// otherwise the session's base model. Zero-config always works.
func (a *Agent) modelFor(effort string) string {
	if m, ok := a.Tiers[effort]; ok && m != "" {
		return m
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
	msgs := a.Sess.Messages
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
			a.Sess.Messages = append(a.Sess.Messages, provider.Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    "Interrupted before this tool ran. Re-issue the call if it is still needed.",
			})
		}
	}
}

func (a *Agent) save() {
	if err := a.Sess.Save(); err != nil && !a.saveWarned {
		fmt.Fprintf(os.Stderr, "\nwarning: could not save session: %v\n", err)
		a.saveWarned = true
	}
}

// record appends a stats line; never fatal, warn once.
func (a *Agent) record(role string, meta provider.Meta, toolCalls int) {
	if a.StatsDir == "" {
		return
	}
	err := stats.Append(a.StatsDir, stats.Record{
		Kind:             "call",
		Session:          a.Sess.ID,
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
	if a.StatsDir == "" {
		return fmt.Errorf("stats are disabled")
	}
	return stats.Append(a.StatsDir, stats.Record{
		Kind:    "rating",
		Session: a.Sess.ID,
		Turn:    a.lastTurnID,
		Rating:  rating,
	})
}

func newTurnID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (a *Agent) confirm(action, detail string) bool {
	if a.Yolo {
		return true
	}
	if a.In == nil {
		return false // no way to ask -> safe default
	}
	fmt.Fprintf(a.Out, "\n%s?%s %s\n%s%s%s\n%sAllow? [y/N]: %s", colorYel, colorReset, action, colorDim, detail, colorReset, colorDim, colorReset)
	line, _ := a.In.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	ok := line == "y" || line == "yes"
	if !ok {
		fmt.Fprintln(a.Out, colorDim+"  skipped."+colorReset)
	}
	return ok
}

// preWrite is the checkpoint hook handed to tools.Execute.
func (a *Agent) preWrite(tool, path string) error {
	if a.Ckpt == nil {
		return nil
	}
	return a.Ckpt.Record(tool, path)
}

func summarizeCall(tc provider.ToolCall) string {
	args := tc.Function.Arguments
	if len(args) > 120 {
		args = args[:120] + "…"
	}
	return fmt.Sprintf("%s(%s)", tc.Function.Name, args)
}

func (a *Agent) footer(meta provider.Meta) {
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
	a.lastTurnID = newTurnID()
	if a.Ckpt != nil {
		a.Ckpt.BeginTurn()
	}
	a.Sess.SetTitleFromInput(userInput)

	if a.Mode == ModeAgent {
		return a.runOrchestrated(ctx, userInput)
	}
	return a.runLoop(ctx, userInput)
}

// runLoop is the chat/code path: stream the reply, execute any tool calls,
// loop until the model returns a plain answer. The session is saved as it
// goes.
func (a *Agent) runLoop(ctx context.Context, userInput string) error {
	a.Sess.Messages = append(a.Sess.Messages, provider.Message{Role: "user", Content: userInput})
	a.save()

	model := a.modelFor(a.Effort)
	toolset := toolsFor(a.Mode)

	for {
		fmt.Fprintf(a.Out, "%sassistant%s ", colorCyan, colorReset)
		msg, meta, err := a.Client.StreamChat(ctx, model, a.Sess.Messages, toolset, func(tok string) {
			fmt.Fprint(a.Out, tok)
		})
		if err != nil {
			fmt.Fprintln(a.Out)
			return err
		}
		fmt.Fprintln(a.Out)
		a.record("main", meta, len(msg.ToolCalls))
		a.Sess.Messages = append(a.Sess.Messages, msg)
		a.save()

		if len(msg.ToolCalls) == 0 {
			a.footer(meta)
			return nil // final answer for this turn
		}

		for _, tc := range msg.ToolCalls {
			fmt.Fprintf(a.Out, "%s  → %s%s\n", colorDim, summarizeCall(tc), colorReset)
			result, err := tools.Execute(ctx, tc.Function.Name, tc.Function.Arguments, a.confirm, a.preWrite)
			if err != nil {
				result = "Error: " + err.Error()
			}
			a.Sess.Messages = append(a.Sess.Messages, provider.Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    result,
			})
		}
		a.save()
		// loop: send tool results back to the model for its next step
	}
}

// Rewind undoes the file changes of the most recent turn (files only; the
// conversation itself is untouched). Returns the restored paths.
func (a *Agent) Rewind() ([]string, error) {
	if a.Ckpt == nil {
		return nil, fmt.Errorf("checkpointing is not enabled")
	}
	return a.Ckpt.RewindLastTurn()
}
