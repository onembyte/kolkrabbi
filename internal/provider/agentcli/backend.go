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
	// startWithOptions is an injectable seam for capability-aware process
	// startup. The legacy start seam remains supported by tests and callers
	// that deliberately use the default process context.
	startWithOptions startLineProcessWithOptions
	execution        ExecutionOptions
	mu               sync.Mutex
	session          *ClaudeSession
	release          context.CancelFunc
}

// NewClaudeBackendFromHandleWithOptions creates a backend that resumes one
// vendor conversation (resume true) or opens a brand-new one kolk has already
// minted a name for, inside an explicit capability envelope. The mode is part
// of the spawn contract: chat runs the vendor with no tool in context, code
// runs the vendor's own tool loop. There used to be an envelope-less form;
// once the session child needed the envelope too (full-auto rides in it),
// nothing in production called it, and one constructor is one rule.
func NewClaudeBackendFromHandleWithOptions(model, mode, effort, handle string, resume bool, options ExecutionOptions) (*ClaudeBackend, error) {
	// Refusing here, before any process exists, is what "says why" means.
	if _, err := claudeModeFlags(mode, options.BypassPermissions); err != nil {
		return nil, err
	}
	options, err := normalizeExecutionOptions(options)
	if err != nil {
		return nil, err
	}
	// The constructor knows which provider it is building for; the caller may
	// not have said. Naming it here is what makes the network rule apply.
	if options.Provider == "" {
		options.Provider = "claude"
	}
	if err := validateExecutionOptions(options); err != nil {
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
		execution: options,
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
	return b.StreamChatObserved(ctx, model, messages, tools, onToken, nil)
}

// StreamChatObserved is StreamChat with optional typed provider boundaries.
func (b *ClaudeBackend) StreamChatObserved(ctx context.Context, model string, messages []provider.Message, tools []provider.Tool, onToken func(string), observe func(provider.ProgressEvent)) (provider.Message, provider.Meta, error) {
	// The tool schemas the gateway seam passes are deliberately ignored: the
	// vendor owns tool execution here, and --allowedTools takes names, not
	// JSON Schema. Pretending to forward them would claim a definition of the
	// vendor's tool loop kolk does not have.
	prompt, err := promptFromMessages(messages)
	if err != nil {
		return provider.Message{}, provider.Meta{Model: model}, err
	}
	// The invocation is built below, after the persistent path has had its
	// chance to return. It used to be built here, and on the persistent path —
	// which is every ordinary session — it was then discarded unused: fifty-odd
	// allocations and a full envelope validation per turn for an argv nobody
	// ran. The one-shot path below still needs it.
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
		message, meta, turnErr := session.TurnObserved(ctx, messages, model, watch, observe)
		// A killed process leaves the vendor's turn unfinished with nothing
		// recorded, and the vendor CONTINUES that turn on the next --resume.
		// Resuming here would let it execute the tool calls kolk has already
		// told the user were cancelled — editing files after a "cancelled"
		// turn, and diverging kolk's transcript from the vendor's permanently.
		// So the conversation is retired rather than reused. Nothing is lost:
		// promptFromMessages sends the whole conversation every turn, so kolk
		// replays its own transcript whether or not the vendor remembers it.
		if session.HardExit() {
			b.forgetHandle()
			b.dropSession(session)
			// Say so — except to the person who just pressed Ctrl-C. §2.5 marks
			// a user cancellation Silent, and they already know why the
			// provider stopped; this line would arrive on every cancellation
			// attached to the thing they deliberately did. Written through
			// onToken rather than watch so it does not count as answer content:
			// `streamed` decides whether a turn may be retried, and a notice is
			// not half an answer.
			if onToken != nil && ctx.Err() == nil {
				onToken(retirementTrail())
			}
		}
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
					message, meta, turnErr = replacement.TurnObserved(ctx, messages, model, watch, observe)
					if replacement.Unusable() {
						b.dropSession(replacement)
					}
				}
			}
		}
		return message, meta, turnErr
	}
	// One-shot: no session process, so this turn is its own invocation.
	var invocation ClaudeInvocation
	if executionOptionsEmpty(b.execution) {
		invocation, err = BuildClaudeInvocation(model, b.Mode, b.Effort, prompt)
	} else {
		invocation, err = BuildClaudeInvocationWithOptions(model, b.Mode, b.Effort, prompt, b.execution)
	}
	if err != nil {
		return provider.Message{}, provider.Meta{Model: model}, err
	}
	start := time.Now()
	events := make([]Event, 0, 8)
	progressPending := make(map[string]string)
	runner := b.run
	run := RunClaude
	if runner != nil {
		run = func(ctx context.Context, invocation ClaudeInvocation, onEvent func(Event)) error {
			return runClaude(ctx, invocation, runner, onEvent)
		}
	}
	err = run(ctx, invocation, func(event Event) {
		events = append(events, event)
		observeProviderEvent(observe, event, progressPending)
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
	args, err := BuildClaudeSessionArgsWithOptions(b.Model, b.Mode, b.Effort, b.handle, resume, b.execution)
	if err != nil {
		release()
		return nil, err
	}
	var process lineProcess
	if b.startWithOptions != nil {
		process, err = b.startWithOptions(sessionContext, "claude", args, shell.ProcessOptions{Dir: b.execution.Workspace})
	} else if b.execution.Workspace != "" {
		process, err = shell.StartLinesProcessWithOptions(sessionContext, "claude", args, shell.ProcessOptions{Dir: b.execution.Workspace})
	} else {
		process, err = b.start(sessionContext, "claude", args)
	}
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
