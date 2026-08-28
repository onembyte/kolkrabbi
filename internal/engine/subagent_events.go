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
func (a *Agent) subagentTaskID(index int) string {
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
	if a.Bus == nil || index < 0 || index >= len(tasks) {
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
