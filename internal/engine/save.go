package engine

import (
	"fmt"
	"sync"
	"time"
)

// defaultSaveInterval bounds how long the tool loop may go without writing the
// transcript when nothing on disk changed.
//
// Two seconds is chosen against what a person can lose rather than against what
// a disk can do: a crash between two interval saves costs at most two seconds
// of chat-only messages, which is less than the time it takes to notice the
// crash. Every boundary that matters — a turn ending, a pause, a permission
// prompt, a tool about to touch a file — flushes regardless, so the interval
// never decides whether a *decision* is durable, only how stale a paragraph of
// streamed prose may be.
const defaultSaveInterval = 2 * time.Second

// saveReason names the moment that asked for a save.
//
// Before O3 the engine had one verb — save() — at sixteen call sites, so a
// 50-round turn on a multi-megabyte session performed roughly a hundred full
// rewrites, each with two fsyncs. The reason is what lets the table below say
// which of those hundred are boundaries a person could notice losing and which
// are bookkeeping that the next boundary will carry anyway.
type saveReason int

const (
	saveUserMessage saveReason = iota
	saveAssistantMessage
	saveToolRound
	saveTurnEnd
	saveFileWrite
	savePermissionPrompt
	savePause
	saveResume
	saveUndo
	saveCompact
	saveChainSwitch
	saveSystemPrompt
	saveAutoTitle
	saveOrchestratorStep
)

// saveRule is one row of the decision.
//
// durable means the bytes are on disk, directory entry and all, before the call
// returns. interval means the write may be coalesced with the ones around it
// and may skip the directory fsync. A reason that is neither only marks the
// transcript dirty and lets the next boundary carry it.
type saveRule struct {
	// name appears in the one warning a failed save is allowed to print, so a
	// user who loses a save knows which moment lost it.
	name     string
	durable  bool
	interval bool
}

// saveRules is the table. Read it as the answer to one question per row: "if
// the machine died immediately after this, would the user have to be told?"
//
// The durable rows are the ones where the answer is yes — the state changed in
// a way the person either asked for (undo, compact, resume) or must be able to
// reason about after a crash (a pause that stops spending, a permission prompt
// they are about to answer, a tool about to change their files). The rest are
// the transcript growing, which the turn's own end makes durable.
var saveRules = [...]saveRule{
	saveUserMessage:      {name: "user message"},
	saveAssistantMessage: {name: "assistant message"},
	// The only interval row: the tool results of one round. This is the row
	// that a 50-round turn walks fifty times.
	saveToolRound:        {name: "tool round", interval: true},
	saveTurnEnd:          {name: "turn end", durable: true},
	saveFileWrite:        {name: "file write", durable: true},
	savePermissionPrompt: {name: "permission prompt", durable: true},
	savePause:            {name: "pause", durable: true},
	saveResume:           {name: "resume", durable: true},
	saveUndo:             {name: "undo", durable: true},
	saveCompact:          {name: "compaction", durable: true},
	saveChainSwitch:      {name: "continuity switch", durable: true},
	saveSystemPrompt:     {name: "system prompt", durable: true},
	saveAutoTitle:        {name: "automatic title"},
	saveOrchestratorStep: {name: "orchestration step"},
}

// saveState is the coalescing bookkeeping. It is a separate struct with its own
// mutex because tool goroutines reach it: with no isolator, orchestrated tasks
// share the session's root and so share the pre-write hook.
type saveState struct {
	mu sync.Mutex
	// pending is set by every mutation and cleared by every write.
	pending bool
	// wroteFile records that a tool changed the working tree since the last
	// write, which promotes the round's own save from interval to durable: the
	// transcript must not be staler than the files it describes, or /undo has
	// a checkpoint whose turn the transcript never mentions.
	wroteFile bool
	// last is when the transcript last reached disk, by either route.
	last time.Time
	// warned keeps a failing disk to one line rather than one per save. It
	// lives here rather than on the Agent because O3 made the pre-write hook a
	// flush point, and with no isolator every orchestrated task shares it: two
	// tasks meeting a read-only disk at once used to race on this flag.
	warned bool
}

// markDirty records that the session changed without writing anything.
//
// This is the cheap half of O3 and the reason a 50-round turn stops costing a
// hundred rewrites: the transcript in memory is already correct, and the next
// boundary — which is at most one save interval away, and always arrives
// before the turn ends — is what makes it correct on disk.
func (a *Agent) markDirty() {
	if a.Sess == nil {
		return
	}
	a.saveState.mu.Lock()
	a.saveState.pending = true
	a.saveState.mu.Unlock()
}

// flush writes the transcript now, durably, if anything is pending.
//
// reason is not decoration: it is what a failed save names, and it is what
// makes the "at most one write per interval" claim checkable, because every
// write in the engine goes through here or through saveInterim.
func (a *Agent) flush(reason saveReason) {
	if a.Sess == nil || !a.takePending() {
		return
	}
	a.writeSession(reason, a.Sess.Save)
}

// saveFor is the single replacement for the sixteen save() calls: it looks the
// moment up in saveRules and either writes or marks.
func (a *Agent) saveFor(reason saveReason) {
	if a.Sess == nil {
		return
	}
	a.markDirty()
	rule := saveRules[reason]
	switch {
	case rule.durable, a.saveInterval() <= 0:
		// A non-positive interval is the rollback switch named in
		// OPTIMIZATION_PLAN.md O3: every save reaches disk, which is exactly
		// the behaviour this leaf replaced.
		a.flush(reason)
	case rule.interval:
		a.saveInterim(reason)
	}
}

// saveInterim is the tool loop's save: at most one per interval, and a durable
// one instead when the round changed the user's files.
func (a *Agent) saveInterim(reason saveReason) {
	now := a.now()
	a.saveState.mu.Lock()
	switch {
	case !a.saveState.pending:
		a.saveState.mu.Unlock()
		return
	case a.saveState.wroteFile:
		a.saveState.pending, a.saveState.wroteFile = false, false
		a.saveState.last = now
		a.saveState.mu.Unlock()
		a.writeSession(saveFileWrite, a.Sess.Save)
		return
	case !a.saveState.last.IsZero() && now.Sub(a.saveState.last) < a.saveInterval():
		// Inside the interval and nothing on disk changed: the transcript
		// stays dirty and the next boundary carries it.
		a.saveState.mu.Unlock()
		return
	}
	a.saveState.pending = false
	a.saveState.last = now
	a.saveState.mu.Unlock()
	a.writeSession(reason, a.Sess.SaveInterim)
}

// noteFileWrite is called from the pre-write hook, before a tool changes the
// tree. It is deliberately separate from the flush that hook performs: the
// flush makes the *tool call* durable before the files move, and this makes the
// round's own save durable afterwards, so the *result* is too.
func (a *Agent) noteFileWrite() {
	a.saveState.mu.Lock()
	a.saveState.wroteFile = true
	a.saveState.mu.Unlock()
}

// takePending claims the pending flag for one writer, so two goroutines
// arriving at the same boundary produce one write rather than two.
func (a *Agent) takePending() bool {
	a.saveState.mu.Lock()
	defer a.saveState.mu.Unlock()
	if !a.saveState.pending {
		return false
	}
	a.saveState.pending, a.saveState.wroteFile = false, false
	a.saveState.last = a.now()
	return true
}

// writeSession performs one write and reports a failure once per session.
func (a *Agent) writeSession(reason saveReason, write func() error) {
	err := write()
	if err == nil {
		return
	}
	a.saveState.mu.Lock()
	first := !a.saveState.warned
	a.saveState.warned = true
	a.saveState.mu.Unlock()
	if !first {
		return
	}
	// Through Out, not os.Stderr: in a session Out is the terminal renderer,
	// which owns the screen, and anything printed around it lands outside the
	// rows it manages and scribbles over the composer.
	fmt.Fprintf(a.Out, "\nwarning: could not save session at the %s: %v\n", saveRules[reason].name, err)
}

// saveInterval is Options.SaveInterval with its default applied. It is read
// here rather than defaulted in New so that an Agent built as a struct literal
// — which most of this package's tests are — coalesces exactly like one the
// host constructs.
func (a *Agent) saveInterval() time.Duration {
	if a.SaveInterval == 0 {
		return defaultSaveInterval
	}
	return a.SaveInterval
}

// now is the engine's clock, which tests replace.
func (a *Agent) now() time.Time {
	if a.Clock != nil {
		return a.Clock()
	}
	return time.Now()
}
