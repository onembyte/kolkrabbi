// Package agentcli defines credential-blind invocations of provider-owned
// agent CLIs. It does not execute processes or inspect provider state.
package agentcli

import (
	"fmt"
	"strings"
)

// ClaudeInvocation is the safe argv envelope for one Claude subscription turn.
// Prompt is supplied separately by the caller through stdin.
type ClaudeInvocation struct {
	Args   []string
	Prompt string
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
