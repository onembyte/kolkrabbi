// Package agentcli defines credential-blind invocations of provider-owned
// agent CLIs. It does not execute processes or inspect provider state.
package agentcli

import (
	"context"
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
func BuildClaudeInvocation(model, effort, prompt string) (ClaudeInvocation, error) {
	if strings.TrimSpace(prompt) == "" {
		return ClaudeInvocation{}, fmt.Errorf("claude prompt cannot be empty")
	}
	args, err := claudeArgs(model, effort, false)
	if err != nil {
		return ClaudeInvocation{}, err
	}
	return ClaudeInvocation{Args: args, Prompt: prompt}, nil
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

// claudeArgs builds the flags common to both one-shot and persistent sessions.
// The one-shot form appends the prompt positionally; the persistent one keeps
// reading stream-json on stdin, which streamOnly expresses with --input-format.
func claudeArgs(model, effort string, streamOnly bool) ([]string, error) {
	effort = strings.ToLower(strings.TrimSpace(effort))
	if !ClaudeEffortValid(effort) {
		return nil, fmt.Errorf("claude has no %q effort level; use low, medium, high, xhigh or max", effort)
	}
	args := []string{"-p", "--verbose", "--output-format", "stream-json"}
	if streamOnly {
		args = append(args, "--input-format", "stream-json")
	}
	args = append(args, "--safe-mode", "--setting-sources", "")
	if model = strings.TrimSpace(model); model != "" {
		args = append(args, "--model", ClaudeVendorModel(model))
	}
	if effort != "" {
		args = append(args, "--effort", effort)
	}
	return args, nil
}
