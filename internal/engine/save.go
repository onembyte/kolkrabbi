package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/onembyte/kolkrabbi/internal/bus"
	"github.com/onembyte/kolkrabbi/internal/secret"
	"github.com/onembyte/kolkrabbi/internal/xid"
	"github.com/onembyte/kolkrabbi/protocol"
)

// RecoverySaveError means an exceptional boundary exists only in memory. It
// is kept distinct so RunTurn does not retry the same failed write or let its
// ordinary end-of-turn flush masquerade as the compressed recovery point.
type RecoverySaveError struct {
	Reason string
	Err    error
}

func (e *RecoverySaveError) Error() string {
	return fmt.Sprintf("could not save the %s recovery point: %v; restart recovery is not guaranteed", e.Reason, secret.Scrub(e.Err.Error()))
}

func (e *RecoverySaveError) Unwrap() error { return e.Err }

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
	// warned keeps a failing disk to one line per failing streak rather than
	// one per save; a successful write clears it, so a disk that recovers and
	// fails again is reported again. It lives here rather than on the Agent
	// because O3 made the pre-write hook a flush point, and with no isolator
	// every orchestrated task shares it: two tasks meeting a read-only disk at
	// once used to race on this flag.
	warned bool
	// recoveryLost holds ordinary writes back from the moment a compressed
	// recovery write fails until its turn has unwound. The deferred stores on
	// the way out would otherwise mark the session dirty again and the turn's
	// end would flush a JSON copy of the very boundary that did not reach disk.
	recoveryLost bool
}

// reset returns the coalescer to what a fresh agent's holds.
func (s *saveState) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending, s.wroteFile, s.last, s.warned, s.recoveryLost = false, false, time.Time{}, false, false
}

// endRecoveryHold lets ordinary writes through again once the turn whose
// recovery write failed has unwound; the state in memory is kept, and the
// next ordinary save persists it without a boundary's claim.
func (s *saveState) endRecoveryHold() {
	s.mu.Lock()
	s.recoveryLost = false
	s.mu.Unlock()
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
	if !a.saveState.recoveryLost {
		a.saveState.pending = true
	}
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

// writeSession performs one write and reports a failure once per failing
// streak. A failed pause is reported every time: the pause's own message
// promises a later /resume, which holds only while this session runs.
func (a *Agent) writeSession(reason saveReason, write func() error) {
	err := write()
	a.saveState.mu.Lock()
	if err == nil {
		a.saveState.warned = false
		a.saveState.mu.Unlock()
		return
	}
	first := !a.saveState.warned
	a.saveState.warned = true
	a.saveState.mu.Unlock()
	if !first && reason != savePause {
		return
	}
	// Through Out, not os.Stderr: in a session Out is the terminal renderer,
	// which owns the screen, and anything printed around it lands outside the
	// rows it manages and scribbles over the composer.
	fmt.Fprintf(a.Out, "\nwarning: could not save session at the %s: %v\n", saveRules[reason].name, err)
	if reason == savePause {
		fmt.Fprintln(a.Out, "warning: the paused turn is kept only until this session exits")
	}
}

// saveRecovery freezes the execution in the session and writes the compressed
// recovery point. It returns the failure because callers must close admission
// and withhold a durable-pause claim when this boundary did not reach disk.
func (a *Agent) saveRecovery(reason string) error {
	if a.Sess == nil {
		return a.recoveryWriteFailed(reason, fmt.Errorf("session storage is unavailable"))
	}
	a.executionMu.Lock()
	prior := ""
	if a.execution != nil {
		prior = a.execution.Recovery
		a.execution.Recovery = reason
	}
	a.executionMu.Unlock()
	a.storeExecution()
	// SaveRecovery writes all current state, so it satisfies the dirty work the
	// ordinary coalescer was carrying. On failure that work deliberately stays
	// out of the turn-end Save path: a JSON write is not the promised complete
	// compressed boundary.
	_ = a.takePending()
	err := a.Sess.SaveRecovery(reason)
	// The compressed file is the recovery point. A mirror or header that
	// failed after it landed is a warning, not a lost boundary.
	var durable interface{ RecoveryDurable() bool }
	if err != nil && errors.As(err, &durable) && durable.RecoveryDurable() {
		a.notifyRecoveryFailure(reason, err.Error(), true)
		err = nil
	}
	a.saveState.mu.Lock()
	if err == nil {
		a.saveState.warned = false
		a.saveState.mu.Unlock()
		return nil
	}
	a.saveState.mu.Unlock()
	// A later ordinary save must not give a failed recovery write provenance.
	a.executionMu.Lock()
	if a.execution != nil {
		a.execution.Recovery = prior
		a.Sess.SetRunState(a.execution)
	}
	a.executionMu.Unlock()
	return a.recoveryWriteFailed(reason, err)
}

func (a *Agent) recoveryWriteFailed(reason string, err error) error {
	a.saveState.mu.Lock()
	a.saveState.pending = false
	a.saveState.warned, a.saveState.recoveryLost = true, true
	a.saveState.mu.Unlock()
	recoveryErr := &RecoverySaveError{Reason: reason, Err: err}
	a.notifyRecoveryFailure(reason, recoveryErr.Error(), false)
	return recoveryErr
}

// notifyRecoveryFailure uses the same scrubbed message for humans and machines.
// A durable sidecar with a failed mirror is still a write failure, but does not
// claim the recovery point itself was lost. Every failed write emits a notice.
func (a *Agent) notifyRecoveryFailure(reason, message string, durable bool) {
	message = secret.Scrub(message)
	fmt.Fprintf(a.Out, "\nwarning: %s\n", message)
	if surface, ok := a.Out.(interface{ RecoveryWarning(string) }); ok {
		surface.RecoveryWarning("warning: " + message)
	}
	if a.Bus != nil {
		turn := a.lastTurnID
		if turn == "" {
			turn = xid.New(xid.Turn)
		}
		data, _ := json.Marshal(protocol.RecoveryFailedData{Code: "recovery_save_failed", Reason: reason, Message: message, Durable: durable})
		_, _ = a.Bus.Publish(bus.Event{Turn: turn, Type: protocol.EventRecoveryFailed, Data: data})
	}
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
