package agentcli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// ClaudeBackend adapts a provider-owned Claude CLI to the engine chat seam.
// Claude's own process remains responsible for authentication and tools.
type ClaudeBackend struct {
	Effort string
	run    lineRunner
}

func (b ClaudeBackend) StreamChat(ctx context.Context, model string, messages []provider.Message, tools []provider.Tool, onToken func(string)) (provider.Message, provider.Meta, error) {
	if len(tools) > 0 {
		return provider.Message{}, provider.Meta{Model: model}, fmt.Errorf("claude provider-owned tools are not yet supported by this adapter")
	}
	prompt, err := promptFromMessages(messages)
	if err != nil {
		return provider.Message{}, provider.Meta{Model: model}, err
	}
	invocation, err := BuildClaudeInvocation(model, b.Effort, prompt)
	if err != nil {
		return provider.Message{}, provider.Meta{Model: model}, err
	}
	start := time.Now()
	events := make([]Event, 0, 8)
	runner := b.run
	run := RunClaude
	if runner != nil {
		run = func(ctx context.Context, invocation ClaudeInvocation, onEvent func(Event)) error {
			return runClaude(ctx, invocation, runner, onEvent)
		}
	}
	err = run(ctx, invocation, func(event Event) {
		events = append(events, event)
		if event.Kind == EventMessageDelta && onToken != nil {
			onToken(event.Text)
		}
	})
	if err != nil {
		return provider.Message{}, provider.Meta{Model: model, Elapsed: time.Since(start)}, err
	}
	message, meta, err := Collect(events, time.Since(start))
	if meta.Model == "" {
		meta.Model = model
	}
	return message, meta, err
}

func promptFromMessages(messages []provider.Message) (string, error) {
	var b strings.Builder
	for _, message := range messages {
		if message.Content == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(strings.ToUpper(message.Role))
		b.WriteString(":\n")
		b.WriteString(message.Content)
	}
	if strings.TrimSpace(b.String()) == "" {
		return "", fmt.Errorf("claude requires at least one non-empty message")
	}
	return b.String(), nil
}
