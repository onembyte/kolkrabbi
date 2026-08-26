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
		return fmt.Errorf("Claude event handler is required")
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
		return ClaudeInvocation{}, fmt.Errorf("Claude prompt cannot be empty")
	}
	args := []string{
		"-p",
		"--verbose",
		"--output-format", "stream-json",
		"--safe-mode",
		"--setting-sources", "",
	}
	if strings.TrimSpace(model) != "" {
		args = append(args, "--model", model)
	}
	if strings.TrimSpace(effort) != "" {
		args = append(args, "--effort", effort)
	}
	return ClaudeInvocation{Args: args, Prompt: prompt}, nil
}
