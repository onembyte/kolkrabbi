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
	// Model, Mode and Effort are the values the provider process is started
	// with. A stream-json process replays no argv, so changing any of them
	// means a new process — the backend is rebuilt by the caller, not mutated.
	Model  string
	Mode   string
	Effort string
	// handle is the vendor conversation this backend drives: minted by
	// Kolkrabbi, carried across backends and restarts through the session
	// file, and reported back through ProviderHandle. resume records that the
	// handle names a conversation kolk already knows about, and started that
	// at least one process has opened it, so a later spawn resumes rather
	// than re-opens.
	handle  string
	resume  bool
	started bool
	run     lineRunner
	start   startLineProcess
	mu      sync.Mutex
	session *ClaudeSession
	release context.CancelFunc
}

// NewClaudeBackendFromHandle creates a backend that resumes one vendor
// conversation (resume true) or opens a brand-new one kolk has already
// minted a name for. The mode is part of the spawn contract: chat runs the
// vendor with no tool in context, code runs the vendor's own tool loop.
func NewClaudeBackendFromHandle(model, mode, effort, handle string, resume bool) (*ClaudeBackend, error) {
	// Refusing here, before any process exists, is what "says why" means.
	if _, err := claudeModeFlags(mode); err != nil {
		return nil, err
	}
	return &ClaudeBackend{
		Model:  model,
		Mode:   strings.ToLower(strings.TrimSpace(mode)),
		Effort: effort,
		handle: handle,
		resume: resume,
		start: func(ctx context.Context, executable string, args []string) (lineProcess, error) {
			return shell.StartLinesProcess(ctx, executable, args)
		},
	}, nil
}

// ProviderHandle reports the vendor conversation this backend has driven most
// recently: a minted handle until the vendor confirms one, then the vendor's
// own report. It is what the session file stores so a later process can
// --resume.
func (b *ClaudeBackend) ProviderHandle() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.session != nil && b.session.ProviderHandle() != "" {
		return b.session.ProviderHandle()
	}
	return b.handle
}

func (b *ClaudeBackend) StreamChat(ctx context.Context, model string, messages []provider.Message, tools []provider.Tool, onToken func(string)) (provider.Message, provider.Meta, error) {
	// The tool schemas the gateway seam passes are deliberately ignored: the
	// vendor owns tool execution here, and --allowedTools takes names, not
	// JSON Schema. Pretending to forward them would claim a definition of the
	// vendor's tool loop kolk does not have.
	prompt, err := promptFromMessages(messages)
	if err != nil {
		return provider.Message{}, provider.Meta{Model: model}, err
	}
	invocation, err := BuildClaudeInvocation(model, b.Mode, b.Effort, prompt)
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
		// the rest of the Kolkrabbi session. But a process that was opened with
		// --resume and produced nothing before dying is the signature of a
		// handle the vendor no longer keeps (the transcripts expire after 30
		// days, or the process died before its conversation was created), so
		// the handle is dropped along with the process: the retry below
		// mints a fresh one instead of resuming the same dead one, and the
		// stale handle never wedges the rest of the Kolkrabbi session.
		if session.Unusable() {
			retrying := turnErr != nil && !streamed && ctx.Err() == nil
			if retrying && session.Resumed() {
				b.forgetHandle()
			}
			b.dropSession(session)
			// The process was already gone when this turn began — the previous
			// turn ended it, which is what an expired login looks like from
			// here. Without this retry the user signs in again, sends a turn,
			// and gets "claude exited before finishing the turn" for their
			// trouble; only the turn after that works. Nothing was streamed, so
			// one attempt on a fresh process is invisible and costs a turn
			// that had already failed.
			if retrying {
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
	// Kolkrabbi mints the handle before the process exists, so a child that
	// dies before its first init frame still leaves a name the next one can
	// resume.
	if b.handle == "" {
		b.handle = NewVendorHandle()
	}
	resume := b.started || b.resume
	args, err := BuildClaudeSessionArgs(b.Model, b.Mode, b.Effort, b.handle, resume)
	if err != nil {
		release()
		return nil, err
	}
	process, err := b.start(sessionContext, "claude", args)
	if err != nil {
		release()
		return nil, err
	}
	b.session = &ClaudeSession{
		process: process,
		model:   b.Model,
		effort:  b.Effort,
		resumed: resume,
	}
	b.release = release
	b.started = true
	return b.session, nil
}

// forgetHandle retires the vendor conversation handle: the next process mints
// a fresh one under --session-id instead of resuming a conversation the
// vendor has already forgotten.
func (b *ClaudeBackend) forgetHandle() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handle = ""
	b.resume = false
	b.started = false
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
