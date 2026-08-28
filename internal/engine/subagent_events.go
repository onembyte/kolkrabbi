package engine

import (
	"encoding/json"

	"github.com/onembyte/kolkrabbi/internal/bus"
	"github.com/onembyte/kolkrabbi/internal/xid"
	"github.com/onembyte/kolkrabbi/protocol"
)

// The protocol has defined subagent.started and subagent.finished since A7, and
// until now nothing published either. That is why an orchestrated run could not
// say how wide it had gone: the information never left the engine.
//
// These are the publisher. They are deliberately the only thing this file does —
// a subagent's *work* is the orchestrator's business, and its *visibility* is
// this one.

// RunningSubagents is how many subagents are working right now.
//
// It is what A33.1's events were for: the count answers the two questions a
// person actually asks of a long orchestrated run — is this still working, and
// how wide did it go. Item 29's test applies and this passes it; anything more
// (per-agent progress, tokens, elapsed) is a number nobody would act on.
func (a *Agent) RunningSubagents() int {
	a.subagentMu.Lock()
	defer a.subagentMu.Unlock()
	return a.subagentRunning
}

// noteSubagents adjusts the count and tells whoever is watching.
//
// runOneTask runs in a goroutine per task, so this is written from several at
// once; the observer is called outside the lock, because a slow renderer must
// not be able to stall the run it is describing.
func (a *Agent) noteSubagents(delta int) {
	a.subagentMu.Lock()
	a.subagentRunning += delta
	if a.subagentRunning < 0 {
		a.subagentRunning = 0
	}
	running := a.subagentRunning
	a.subagentMu.Unlock()

	if a.Agents != nil {
		a.Agents(running)
	}
}

// subagentTaskID is the canonical id for one task in the current turn.
//
// Minted once per index and remembered, so the start and the finish of one task
// carry the same id. A reader that cannot pair the two events is a reader whose
// count never comes back down — which is the specific way this feature would
// fail while still looking like it worked.
//
// The memo is cleared when the turn changes, because a task index means nothing
// across turns and a stale entry would pair this turn's finish with the last
// turn's start.
//
// It is called from the per-task goroutines, so it takes the same lock the
// count does. Without it two tasks starting together read and write the memo at
// once, which the race detector catches and which a real run can turn into a
// concurrent map write -- a panic mid-turn, not a wrong number.
func (a *Agent) subagentTaskID(index int) string {
	a.subagentMu.Lock()
	defer a.subagentMu.Unlock()
	if a.subagentIDTurn != a.lastTurnID {
		a.subagentIDTurn = a.lastTurnID
		a.subagentIDs = nil
	}
	if id, minted := a.subagentIDs[index]; minted {
		return id
	}
	if a.subagentIDs == nil {
		a.subagentIDs = map[int]string{}
	}
	id := xid.New(xid.Task)
	a.subagentIDs[index] = id
	return id
}

// publishSubagentStarted announces one child turn.
func (a *Agent) publishSubagentStarted(tasks []Task, index int, childTurn string) {
	if index < 0 || index >= len(tasks) {
		return
	}
	// Counted before the bus check: the count is a separate consumer, and a
	// session with no bus still has a person watching the composer.
	a.noteSubagents(1)
	if a.Bus == nil {
		return
	}
	title := tasks[index].Title
	if title == "" {
		// The contract refuses an empty task, and an unlabelled one is still a
		// running agent that the count must include.
		title = "task " + itoa(index+1)
	}
	data, err := json.Marshal(protocol.SubagentStartedData{
		ID:        a.subagentTaskID(index),
		ChildTurn: childTurn,
		Task:      title,
		Mode:      a.Mode,
		Index:     index + 1, // the contract is one-based; the slice is not
		Total:     len(tasks),
	})
	if err != nil {
		return
	}
	_, _ = a.Bus.Publish(bus.Event{
		Turn: a.lastTurnID,
		Type: protocol.EventSubagentStarted,
		Data: data,
	})
}

// publishSubagentFinished records how one child turn ended.
//
// Published on every path out of a task, including failure: an event that only
// fires on success leaves a counter stuck at a number that never comes down,
// which is worse than no counter at all.
func (a *Agent) publishSubagentFinished(childTurn string, index int, ok bool) {
	a.noteSubagents(-1)
	if a.Bus == nil {
		return
	}
	data, err := json.Marshal(protocol.SubagentFinishedData{
		ID:        a.subagentTaskID(index),
		ChildTurn: childTurn,
		Mode:      a.Mode,
		OK:        ok,
	})
	if err != nil {
		return
	}
	_, _ = a.Bus.Publish(bus.Event{
		Turn: a.lastTurnID,
		Type: protocol.EventSubagentFinished,
		Data: data,
	})
}
