package engine

import "context"

// ReplaceSession puts the agent on sess: /new and /clear. What belongs to a
// session returns to what New gives a fresh agent: the turn, the run and its
// hops, the spend, the context measure, compaction, plan meters, the work
// ledger and the pause claim. What belongs to the process stays: the options,
// the extra tool servers already started, every lock, and the resume
// machinery the surface armed, so a pause in the new session is watched as
// the old one's was. TestEveryAgentFieldIsClassifiedForANewSession holds the
// list.
//
// It replaces a whole-struct overwrite that raced every goroutine still
// holding the agent: the pause monitor, background discovery, the status line.
// It runs on the turn's goroutine between turns, as a slash command does. The
// pause being watched belongs to the old session, so the watch and any
// delivery in flight are stopped and joined first (QuiesceResume); the caller
// must not hold anything a delivery waits on.
func (a *Agent) ReplaceSession(sess SessionPort, ckpt Checkpointer) {
	a.QuiesceResume()

	a.sessMu.Lock()
	a.Sess, a.Ckpt = sess, ckpt
	a.sessMu.Unlock()
	// The backend stays, but its vendor conversation belonged to the old
	// session: kept, it would carry the new session's requests into the
	// conversation where the old one's saved work waits.
	if forget, ok := a.SessionBackend().(interface{ ForgetConversation() }); ok {
		forget.ForgetConversation()
	}

	a.resumeMu.Lock()
	a.resumeClaim = ""
	a.resumeMu.Unlock()
	a.mainWorkMu.Lock()
	a.mainWorkTurn, a.mainWorkSequence = "", 0
	a.mainWorkMu.Unlock()
	a.subagentMu.Lock()
	a.subagentIDs, a.subagentStatus, a.subagentIDTurn, a.subagentRunning = nil, nil, "", 0
	a.pausedChildren, a.pausedTasks = nil, nil
	a.subagentMu.Unlock()
	a.slotMu.Lock()
	a.slotChoice = nil
	a.slotMu.Unlock()
	a.limitMu.Lock()
	a.limitDecided, a.limitModel = false, ""
	a.limitMu.Unlock()
	a.planLimitsMu.Lock()
	a.planLimits = nil
	a.planLimitsMu.Unlock()
	a.executionMu.Lock()
	a.execution, a.partialMain, a.partialTask, a.attemptBefore = nil, nil, nil, nil
	a.executionMu.Unlock()
	a.lastPromptTokens.Store(0)
	a.sessionSpend.reset()
	a.saveState.reset()
	// Read and written only on the turn's goroutine.
	a.lastTurnID, a.lastArchive = "", ""
	a.hopsThisRun, a.askedThisRun = 0, false
	a.preCompact, a.postCompact, a.runSpend = nil, nil, nil

	a.adoptSession()
}

// QuiesceResume ends the current generation of pause watching: every monitor
// and delivery is cancelled, and every monitor goroutine joined for its whole
// life, the read that decides whether to watch again included. The machinery
// is not retired: a fresh generation starts from the surface's context, so
// the next pause is watched as before. A cancelled delivery leaves its pause
// on the session it came from, for a later /resume.
//
// It must not run concurrently with WatchPauses, which starts a generation of
// its own: both surfaces call WatchPauses once, before any input can arrive,
// and QuiesceResume only from a command.
func (a *Agent) QuiesceResume() {
	a.resumeMu.Lock()
	if a.resumeStop == nil {
		a.resumeMu.Unlock()
		return // nothing was ever watched
	}
	// Cancelled under resumeMu, where armResume checks the generation and
	// counts its goroutine: none is added while the wait below runs.
	a.resumeStop()
	a.resumeMu.Unlock()
	a.resumeRunning.Wait()
	a.resumeMu.Lock()
	if !a.resumeClosed && a.resumeParent.Err() == nil {
		a.resumeBase, a.resumeStop = context.WithCancel(a.resumeParent)
	}
	a.resumeMu.Unlock()
}

// Session is the session the agent is on, safe to ask from any goroutine: a
// reader off the turn's goroutine must not meet ReplaceSession mid-swap.
func (a *Agent) Session() SessionPort {
	a.sessMu.RLock()
	defer a.sessMu.RUnlock()
	return a.Sess
}
