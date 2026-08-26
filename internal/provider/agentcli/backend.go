package agentcli

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/shell"
)

// ClaudeBackend adapts a provider-owned Claude CLI to the engine chat seam.
// Claude's own process remains responsible for authentication and tools.
type ClaudeBackend struct {
	Effort  string
	run     lineRunner
	start   startLineProcess
	mu      sync.Mutex
	session *ClaudeSession
}

// NewClaudeBackend creates a backend that lazily owns one persistent provider
// process for its lifetime.
func NewClaudeBackend(effort string) *ClaudeBackend {
	return &ClaudeBackend{
		Effort: effort,
		start: func(ctx context.Context, executable string, args []string) (lineProcess, error) {
			return shell.StartLinesProcess(ctx, executable, args)
		},
	}
}

func (b *ClaudeBackend) StreamChat(ctx context.Context, model string, messages []provider.Message, tools []provider.Tool, onToken func(string)) (provider.Message, provider.Meta, error) {
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
	if b.start != nil {
		session, err := b.getSession(ctx)
		if err != nil {
			return provider.Message{}, provider.Meta{Model: model}, err
		}
		return session.Turn(ctx, messages, model, onToken)
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

func (b *ClaudeBackend) getSession(ctx context.Context) (*ClaudeSession, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.session != nil {
		return b.session, nil
	}
	process, err := b.start(ctx, "claude", BuildClaudeSessionArgs(b.Effort))
	if err != nil {
		return nil, err
	}
	b.session = &ClaudeSession{process: process, effort: b.Effort}
	return b.session, nil
}

// Close releases the provider process owned by this backend.
func (b *ClaudeBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.session == nil {
		return nil
	}
	return b.session.Close()
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
