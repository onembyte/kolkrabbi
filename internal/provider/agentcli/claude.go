// Package agentcli defines credential-blind invocations of provider-owned
// agent CLIs. It does not execute processes or inspect provider state.
package agentcli

import (
	"context"
	cryptorand "crypto/rand"
	"fmt"
	"io"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/shell"
)

// ClaudeInvocation is the safe argv envelope for one Claude subscription turn.
// Prompt is supplied separately by the caller through stdin.
type ClaudeInvocation struct {
	Args   []string
	Prompt string
}

type lineRunner func(context.Context, string, []string, io.Reader, func([]byte) error) error

// RunClaude translates one provider-owned Claude process into safe events.
func RunClaude(ctx context.Context, invocation ClaudeInvocation, onEvent func(Event)) error {
	return runClaude(ctx, invocation, shell.RunLines, onEvent)
}

func runClaude(ctx context.Context, invocation ClaudeInvocation, run lineRunner, onEvent func(Event)) error {
	if onEvent == nil {
		return fmt.Errorf("claude event handler is required")
	}
	return run(ctx, "claude", invocation.Args, strings.NewReader(invocation.Prompt+"\n"), func(line []byte) error {
		events, err := Translate(line)
		if err != nil {
			return err
		}
		for _, event := range events {
			onEvent(event)
		}
		return nil
	})
}

// BuildClaudeInvocation creates the documented non-interactive Claude CLI
// invocation. The provider CLI remains responsible for authentication.
func BuildClaudeInvocation(model, mode, effort, prompt string) (ClaudeInvocation, error) {
	if strings.TrimSpace(prompt) == "" {
		return ClaudeInvocation{}, fmt.Errorf("claude prompt cannot be empty")
	}
	args, err := claudeArgs(mode, model, effort, "", false, false)
	if err != nil {
		return ClaudeInvocation{}, err
	}
	return ClaudeInvocation{Args: args, Prompt: prompt}, nil
}

// claudeCodeTools is the vendor tool set every session runs with, in code mode
// and in agent mode alike.
//
// Task stays off in both. The reason is one thing, not two: the vendor's own
// subagent scheduler would put a subagent tree in the stream that kolk's bus
// cannot represent, so kolk could neither record those children nor stop them.
// That is true whoever is orchestrating.
//
// It used to read "kolk's agent mode is not available on this backend at all,
// and …", which was circular — Task was off because agent mode was refused, and
// agent mode was refused because the vendor schedules subagents. Only the
// second half was ever an argument. kolk's agent mode spawns kolk's own
// subagents, each a process kolk starts, records and can kill; that is a
// different thing from the vendor scheduling its own, and this constant is what
// keeps the vendor from doing so.
const claudeCodeTools = "Bash,Read,Edit,Write,Glob,Grep,WebFetch,WebSearch,TodoWrite"

// claudeModeFlags turns kolk's session mode into the vendor flags that make
// it structural. The provider owns its tools — kolk sends schemas to nobody
// here — so "chat cannot touch your files" is enforced by the vendor running
// with no tool in context at all, and code mode works because the vendor's
// own tool loop is on. A mode change therefore changes the process, not the
// request: a stream-json process replays no argv.
func claudeModeFlags(mode string) ([]string, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	// Agent mode takes the same flags as code mode. In an orchestrated run this
	// process serves the planner and the synthesis calls, and the single-task
	// fallback that degrades to the ordinary loop — all of which need the
	// vendor's tool loop exactly as code mode does. Task is absent either way.
	case "", "code", "agent":
		return []string{"--tools", claudeCodeTools, "--permission-mode", "acceptEdits"}, nil
	case "chat":
		return []string{"--tools", "", "--permission-mode", "dontAsk"}, nil
	default:
		return nil, fmt.Errorf("unknown mode %q (chat|code|agent)", mode)
	}
}

// claudeEfforts is the closed set the vendor documents for --effort. The CLI
// itself warn-and-runs on an unknown value, which would leave the effort dial
// silently doing nothing on this backend, so Kolkrabbi refuses here instead —
// refusing what will not take effect beats running what will not be honored.
var claudeEfforts = map[string]bool{"low": true, "medium": true, "high": true, "xhigh": true, "max": true}

// ClaudeEffortValid reports whether one level belongs to the vendor's closed
// effort set.
func ClaudeEffortValid(effort string) bool {
	if effort == "" {
		return true
	}
	return claudeEfforts[strings.ToLower(strings.TrimSpace(effort))]
}

// claudeModelAliases maps Kolkrabbi's plan-catalog display names to the model
// the vendor CLI accepts. Its --model flag takes its own short aliases or full
// ids; a catalog name like "claude-opus" is neither, and an unrecognized model
// fails the whole turn with a clean error result — burning the turn to
// discover it. Full ids pass through untouched.
var claudeModelAliases = map[string]string{
	"claude-sonnet": "sonnet",
	"claude-opus":   "opus",
	"claude-haiku":  "haiku",
	"claude-fable":  "fable",
}

// ClaudeVendorModel translates one plan-catalog name into the vendor's own
// model spelling.
func ClaudeVendorModel(model string) string {
	if alias, ok := claudeModelAliases[strings.ToLower(strings.TrimSpace(model))]; ok {
		return alias
	}
	return model
}

// NewVendorHandle mints the conversation handle a claude session is opened
// under. Kolk mints it so it owns the resume handle before the process
// starts — a child that dies before its first init frame still leaves a name
// for the next one to resume, or for the session file to record.
func NewVendorHandle() string {
	uuid := make([]byte, 16)
	if _, err := cryptorand.Read(uuid); err != nil {
		return ""
	}
	uuid[6] = (uuid[6] & 0x0f) | 0x40
	uuid[8] = (uuid[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16])
}

// claudeArgs builds the flags common to both one-shot and persistent sessions.
// The one-shot form appends the prompt positionally; the persistent one keeps
// reading stream-json on stdin, which streamOnly expresses with --input-format.
// A handle either opens one named conversation (--session-id) or resumes it
// (--resume); the vendor replays no flag vector on resume, so the model and
// effort flags are re-passed alongside it every time.
func claudeArgs(mode, model, effort, handle string, resume, streamOnly bool) ([]string, error) {
	effort = strings.ToLower(strings.TrimSpace(effort))
	if !ClaudeEffortValid(effort) {
		return nil, fmt.Errorf("claude has no %q effort level; use low, medium, high, xhigh or max", effort)
	}
	modeFlags, err := claudeModeFlags(mode)
	if err != nil {
		return nil, err
	}
	args := []string{"-p", "--verbose", "--output-format", "stream-json"}
	if streamOnly {
		args = append(args, "--input-format", "stream-json")
	}
	args = append(args, "--safe-mode", "--setting-sources", "")
	// One comma-separated string per variadic flag: the vendor's variadic
	// flags consume every following bare token, so a second bare token would
	// register as a tool name, not a flag value.
	args = append(args, modeFlags...)
	if model = strings.TrimSpace(model); model != "" {
		args = append(args, "--model", ClaudeVendorModel(model))
	}
	if handle != "" {
		if resume {
			args = append(args, "--resume", handle)
		} else {
			args = append(args, "--session-id", handle)
		}
	}
	if effort != "" {
		args = append(args, "--effort", effort)
	}
	return args, nil
}
