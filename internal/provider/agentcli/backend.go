package agentcli

import (
	"context"
	"errors"
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
	// confirmed is the last handle a vendor frame named. It outlives the
	// process that named it, so a conversation the vendor confirmed stays
	// confirmed after that process dies, until the handle itself is retired.
	confirmed string
	// delivered records that the latest turn's prompt reached a process, and
	// turnClosed that the vendor's result frame ended that turn, whatever became
	// of the process afterwards.
	delivered, turnClosed bool
	// retired records that the conversation was given up (a killed turn, a
	// dead resume, a new session) and no new one has opened yet, so the
	// session file can forget it too.
	retired bool
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

// ProviderHandleConfirmed reports only the vendor's acknowledgement. Claude
// accepts a locally minted --session-id before system/init proves that a
// conversation exists, so a non-empty ProviderHandle alone is insufficient.
// The acknowledgement survives the process that gave it: a handle the vendor
// named stays confirmed until it is retired.
func (b *ClaudeBackend) ProviderHandleConfirmed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.session != nil && b.session.ProviderHandle() != "" {
		return true
	}
	return b.handle != "" && b.handle == b.confirmed
}

// TurnNeverStarted proves the latest turn's prompt never reached a Claude
// process. Once one was delivered, missing output proves nothing: a process
// can act and die before its frames arrive.
func (b *ClaudeBackend) TurnNeverStarted() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.delivered
}

// TurnClosed reports that Claude's own result frame ended the latest turn.
func (b *ClaudeBackend) TurnClosed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.turnClosed
}

// noteTurn records what a session said about the turn it ran: whether its
// prompt was delivered, whether the vendor closed it, and the conversation it
// named. Called before the session can be retired, so none of it is lost.
func (b *ClaudeBackend) noteTurn(session *ClaudeSession) {
	delivered, closed, named := session.Delivered(), session.TurnClosed(), session.ProviderHandle()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.delivered = b.delivered || delivered
	b.turnClosed = closed
	// The vendor's own name for the conversation is the one every later
	// process resumes, as the journal and the session file record it.
	if named != "" {
		b.handle, b.confirmed = named, named
	}
}

// ResumesConversation reports that a confirmed conversation continues with
// --resume. A new message continues it; the interrupted prompt is not resent.
func (b *ClaudeBackend) ResumesConversation() bool { return true }

func (b *ClaudeBackend) StreamChat(ctx context.Context, model string, messages []provider.Message, tools []provider.Tool, onToken func(string)) (provider.Message, provider.Meta, error) {
	return b.StreamChatObserved(ctx, model, messages, tools, onToken, nil)
}

// StreamChatObserved is StreamChat with optional typed provider boundaries.
func (b *ClaudeBackend) StreamChatObserved(ctx context.Context, model string, messages []provider.Message, tools []provider.Tool, onToken func(string), observe func(provider.ProgressEvent)) (provider.Message, provider.Meta, error) {
	// The tool schemas the gateway seam passes are deliberately ignored: the
	// vendor owns tool execution here, and --allowedTools takes names, not
	// JSON Schema. Pretending to forward them would claim a definition of the
	// vendor's tool loop kolk does not have.
	b.mu.Lock()
	b.delivered, b.turnClosed = false, false
	b.mu.Unlock()
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
		b.noteTurn(session)
		// A session that lost its place in the provider stream is replaced
		// rather than kept: one unrecoverable interrupt must not end Claude for
		// the rest of the Kolkrabbi session.
		if session.Unusable() {
			// Only a turn whose prompt never reached the process is tried
			// again, and on the same conversation: a turn never moves to a
			// conversation that did not see what came before it. A delivered
			// one may have run tools, whose frames can be lost with the process
			// or reach observe rather than onToken, so "nothing streamed" is no
			// proof that nothing happened.
			retrying := turnErr != nil && !streamed && !session.Delivered() && ctx.Err() == nil
			last := session
			b.dropSession(session)
			b.retireIfKilled(ctx, session, onToken)
			// The process was already gone when this turn began — the previous
			// turn ended it, which is what an expired login looks like from
			// here. Without this retry the user signs in again, sends a turn,
			// and gets "claude exited before finishing the turn" for their
			// trouble; only the turn after that works. The prompt never
			// reached that process, so one attempt on a fresh one is invisible
			// and cannot repeat anything.
			if retrying {
				if replacement, startErr := b.getSession(ctx); startErr == nil {
					message, meta, turnErr = replacement.TurnObserved(ctx, messages, model, watch, observe)
					b.noteTurn(replacement)
					if replacement.Unusable() {
						b.dropSession(replacement)
					}
					b.retireIfKilled(ctx, replacement, onToken)
					last = replacement
				}
			}
			// A resumed process that answered nothing in its whole life is the
			// signature of a conversation the vendor no longer keeps (its
			// transcripts expire, or the process died before the conversation
			// existed). Its handle goes once this turn has failed, so the next
			// turn opens a fresh one and the stale handle never wedges the
			// session; no turn is moved to it midway.
			if turnErr != nil && last.Resumed() && !last.EverReceived() {
				b.forgetHandle()
			}
		}
		// A session still usable ended its turn with the vendor's result
		// frame, so even a process killed just after it left nothing
		// unfinished: the next turn finds it gone and retries on the same
		// conversation, its prompt never having reached that process.
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
	// The prompt travels in the invocation: once the run is attempted it may
	// have reached a process, unless the process provably never ran.
	b.mu.Lock()
	b.delivered = true
	b.mu.Unlock()
	defer func() {
		var notStarted *shell.NotStartedError
		if errors.As(err, &notStarted) {
			b.mu.Lock()
			b.delivered = false
			b.mu.Unlock()
		}
	}()
	err = run(ctx, invocation, func(event Event) {
		events = append(events, event)
		if event.Kind == EventMessageCompleted {
			b.mu.Lock()
			b.turnClosed = true
			b.mu.Unlock()
		}
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
		b.retired = false
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
	b.retired = true
}

// ProviderHandleRetired reports that the conversation this backend drove was
// retired and no new one has opened since. A retired conversation must not be
// resumed by anything, a later kolk process included.
func (b *ClaudeBackend) ProviderHandleRetired() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.retired
}

// ForgetConversation leaves the conversation this backend drives: its process
// ends and the next turn opens a new conversation. A new kolk session on the
// same backend must not share the old session's conversation, where that
// session's own saved work may wait.
func (b *ClaudeBackend) ForgetConversation() {
	b.mu.Lock()
	session := b.session
	b.mu.Unlock()
	if session != nil {
		b.dropSession(session)
	}
	b.forgetHandle()
}

// retireIfKilled drops a session whose process was killed rather than allowed
// to end its own turn, and retires the conversation when the kill caught a
// delivered turn the vendor had not closed. Such a turn is unfinished with
// nothing recorded, and the vendor CONTINUES it on the next --resume:
// resuming would let it execute the tool calls kolk has already told the user
// were cancelled — editing files after a "cancelled" turn, and diverging
// kolk's transcript from the vendor's permanently. So the conversation is
// retired rather than reused. Nothing is lost: promptFromMessages sends the
// whole conversation every turn, so kolk replays its own transcript whether
// or not the vendor remembers it. A process killed while idle, or after the
// vendor's result frame, ended its last turn, and its conversation is whole.
//
// It is judged after the session is dropped, because dropping closes the
// process, and a close can itself be the kill: a cancelled turn whose vendor
// will not stop is killed after its grace. Every process that ran the turn
// is judged, the retry's replacement included.
func (b *ClaudeBackend) retireIfKilled(ctx context.Context, session *ClaudeSession, onToken func(string)) {
	// Only a session that lost its place in the stream is judged, and such a
	// session never closed its turn: a result frame, drained or read, ends
	// the turn in step. A process killed after the result, the retry's
	// replacement included, therefore retires nothing; the next turn finds it
	// gone and retries on the same conversation.
	if !session.Unusable() || !session.HardExit() {
		return
	}
	b.dropSession(session)
	if !session.Delivered() {
		return
	}
	b.forgetHandle()
	// Say so — except to the person who just pressed Ctrl-C. §2.5 marks a user
	// cancellation Silent, and they already know why the provider stopped;
	// this line would arrive on every cancellation attached to the thing they
	// deliberately did. Written through onToken rather than watch so it does
	// not count as answer content: `streamed` decides whether a turn may be
	// retried, and a notice is not half an answer.
	if onToken != nil && ctx.Err() == nil {
		onToken(retirementTrail())
	}
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
