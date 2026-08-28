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
	release context.CancelFunc
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
		// Whether anything reached the user decides whether this turn can be
		// retried at all: replaying a turn that already streamed half an answer
		// would print it twice.
		streamed := false
		watch := onToken
		if onToken != nil {
			watch = func(token string) {
				streamed = true
				onToken(token)
			}
		}
		message, meta, turnErr := session.Turn(ctx, messages, model, watch)
		// A session that lost its place in the provider stream is replaced
		// rather than kept: one unrecoverable interrupt must not end Claude for
		// the rest of the Kolkrabbi session.
		if session.Unusable() {
			b.dropSession(session)
			// The process was already gone when this turn began — the previous
			// turn ended it, which is what an expired login looks like from
			// here. Without this retry the user signs in again, sends a turn,
			// and gets "claude exited before finishing the turn" for their
			// trouble; only the turn after that works. Nothing was streamed, so
			// one attempt on a fresh process is invisible and costs a turn
			// that had already failed.
			if turnErr != nil && !streamed && ctx.Err() == nil {
				if replacement, startErr := b.getSession(ctx); startErr == nil {
					message, meta, turnErr = replacement.Turn(ctx, messages, model, watch)
					if replacement.Unusable() {
						b.dropSession(replacement)
					}
				}
			}
		}
		return message, meta, turnErr
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
	// The process belongs to the Kolkrabbi session, not to the turn that first
	// needed it. Inheriting the turn context would let one cancelled turn kill
	// Claude for every later turn. Close is the only thing that ends it.
	sessionContext, release := context.WithCancel(context.WithoutCancel(ctx))
	process, err := b.start(sessionContext, "claude", BuildClaudeSessionArgs(b.Effort))
	if err != nil {
		release()
		return nil, err
	}
	b.session = &ClaudeSession{process: process, effort: b.Effort}
	b.release = release
	return b.session, nil
}

// dropSession retires one session so the next turn starts a fresh provider
// process. It is a no-op if the backend already moved on.
func (b *ClaudeBackend) dropSession(session *ClaudeSession) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.session != session {
		return
	}
	_ = b.session.Close()
	if b.release != nil {
		b.release()
		b.release = nil
	}
	b.session = nil
}

// Close releases the provider process owned by this backend.
func (b *ClaudeBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.session == nil {
		return nil
	}
	err := b.session.Close()
	if b.release != nil {
		b.release()
		b.release = nil
	}
	return err
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
