package engine

import (
	"encoding/json"
	"fmt"
	"slices"

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
// The count remains available to compact or non-interactive surfaces; richer
// interactive surfaces consume the typed Subagents lifecycle instead.
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
	saved := a.executionTask(index)
	a.subagentMu.Lock()
	defer a.subagentMu.Unlock()
	if a.subagentIDTurn != a.lastTurnID {
		a.subagentIDTurn = a.lastTurnID
		a.subagentIDs = nil
		a.subagentStatus = nil
	}
	if id, minted := a.subagentIDs[index]; minted {
		return id
	}
	if a.subagentIDs == nil {
		a.subagentIDs = map[int]string{}
	}
	id := xid.New(xid.Task)
	if saved.ID != "" {
		id = saved.ID
	}
	a.subagentIDs[index] = id
	return id
}

func (a *Agent) newSubagentStatus(tasks []Task, index int, childTurn, model, effort string) SubagentStatus {
	summary := tasks[index].Title
	if summary == "" {
		summary = "task " + itoa(index+1)
	}
	return SubagentStatus{
		ID:        a.subagentTaskID(index),
		ChildTurn: childTurn,
		Index:     index + 1,
		Total:     len(tasks),
		Model:     model,
		Effort:    effort,
		Summary:   summary,
		Sequence:  a.executionTask(index).Sequence,
	}
}

func (a *Agent) queueSubagentStatus(tasks []Task, index int, childTurn, model, effort string) SubagentStatus {
	saved := a.executionTask(index)
	status, err := advanceSubagentStatus(
		a.newSubagentStatus(tasks, index, childTurn, model, effort),
		SubagentQueued, SubagentPhaseSchedule, "queued",
	)
	if err != nil {
		return SubagentStatus{}
	}
	if prior, settled := executionOutcome(saved); settled {
		status.State, status.Phase, status.Step = SubagentDone, SubagentPhaseComplete, "saved: "+prior.Status.String()
		switch prior.Status {
		case statusFailed, statusIncomplete:
			status.State = SubagentFailed
		case statusBlocked, statusOverBudget:
			status.State = SubagentBlocked
		}
	} else if saved.Sequence > 0 {
		status.State, status.Step = SubagentWaiting, "continuing saved task"
	}
	a.subagentMu.Lock()
	if a.subagentStatus == nil {
		a.subagentStatus = map[int]SubagentStatus{}
	}
	a.subagentStatus[index] = status
	a.subagentMu.Unlock()
	return status
}

func (a *Agent) startSubagentStatus(tasks []Task, index int, childTurn, model, effort string) SubagentStatus {
	a.subagentMu.Lock()
	status, found := a.subagentStatus[index]
	a.subagentMu.Unlock()
	if !found {
		status = a.newSubagentStatus(tasks, index, childTurn, model, effort)
	}
	status.ChildTurn = childTurn
	status.Model = model
	status.Effort = effort
	status, err := advanceSubagentStatus(status, SubagentWorking, SubagentPhaseSchedule, "started")
	if err != nil {
		return SubagentStatus{}
	}
	a.subagentMu.Lock()
	if a.subagentStatus == nil {
		a.subagentStatus = map[int]SubagentStatus{}
	}
	a.subagentStatus[index] = status
	a.subagentMu.Unlock()
	return status
}

func (a *Agent) updateSubagentStatus(index int, state SubagentState, phase SubagentPhase, step string) bool {
	a.subagentMu.Lock()
	status, found := a.subagentStatus[index]
	if !found {
		a.subagentMu.Unlock()
		return false
	}
	step = compactSubagentStep(step)
	if status.State == state && status.Phase == phase && status.Step == step {
		a.subagentMu.Unlock()
		return false
	}
	next, err := advanceSubagentStatus(status, state, phase, step)
	if err != nil {
		a.subagentMu.Unlock()
		return false
	}
	a.subagentStatus[index] = next
	a.subagentMu.Unlock()
	a.notifySubagent(next)
	return true
}

func (a *Agent) finishSubagentStatus(index int, ok bool, model, effort string) (SubagentStatus, bool) {
	id := a.subagentTaskID(index)
	a.subagentMu.Lock()
	status, found := a.subagentStatus[index]
	if !found {
		status, _ = advanceSubagentStatus(
			SubagentStatus{ID: id, Index: index + 1, Total: index + 1},
			SubagentWorking, SubagentPhaseProvider, "starting provider",
		)
	}
	status.Model = model
	status.Effort = effort
	if status.State == SubagentDone || status.State == SubagentFailed || status.State == SubagentBlocked {
		a.subagentStatus[index] = status
		a.subagentMu.Unlock()
		return status, false
	}
	state := SubagentFailed
	step := "failed"
	if ok {
		state = SubagentDone
		step = "completed"
	}
	status, _ = advanceSubagentStatus(status, state, SubagentPhaseComplete, step)
	if a.subagentStatus == nil {
		a.subagentStatus = map[int]SubagentStatus{}
	}
	a.subagentStatus[index] = status
	a.subagentMu.Unlock()
	return status, true
}

func (a *Agent) updateSubagentStatusRoute(index int, model, effort string) {
	a.subagentMu.Lock()
	status, found := a.subagentStatus[index]
	if found {
		status.Model = model
		status.Effort = effort
		status, _ = advanceSubagentStatus(status, SubagentWorking, SubagentPhaseProvider,
			fmt.Sprintf("falling back to %s", model))
		a.subagentStatus[index] = status
	}
	a.subagentMu.Unlock()
	if found {
		a.notifySubagent(status)
	}
}

func (a *Agent) blockSubagentStatus(index int, reason string) {
	a.updateSubagentStatus(index, SubagentBlocked, SubagentPhaseComplete, reason)
}

func (a *Agent) notifySubagent(status SubagentStatus) {
	a.executionMu.Lock()
	if a.execution != nil && status.Index > 0 && status.Index <= len(a.execution.Tasks) {
		task := &a.execution.Tasks[status.Index-1]
		task.ID, task.Sequence = status.ID, status.Sequence
	}
	a.executionMu.Unlock()
	if a.Subagents != nil {
		a.Subagents(status)
	}
	a.publishSubagentWork(status)
}

func (a *Agent) publishSubagentWork(status SubagentStatus) {
	if a.Bus == nil || status.ID == "" || status.ChildTurn == "" || status.Sequence == 0 {
		return
	}
	data, err := json.Marshal(protocol.WorkUpdatedData{
		ID:        status.ID,
		ChildTurn: status.ChildTurn,
		Role:      protocol.WorkRoleSubagent,
		State:     protocol.WorkState(status.State),
		Phase:     protocol.WorkPhase(status.Phase),
		Step:      status.Step,
		Sequence:  status.Sequence,
		Index:     status.Index,
		Total:     status.Total,
		Model:     status.Model,
		Effort:    status.Effort,
	})
	if err != nil {
		return
	}
	_, _ = a.Bus.Publish(bus.Event{
		Turn: a.lastTurnID,
		Type: protocol.EventWorkUpdated,
		Data: data,
	})
}

// publishSubagentStarted announces one child turn.
func (a *Agent) publishSubagentStarted(tasks []Task, index int, childTurn, model, effort string) {
	if index < 0 || index >= len(tasks) {
		return
	}
	// Counted before the bus check: the count is a separate consumer, and a
	// session with no bus still has a person watching the composer.
	a.noteSubagents(1)
	status := a.startSubagentStatus(tasks, index, childTurn, model, effort)
	if a.Bus == nil {
		a.notifySubagent(status)
		return
	}
	data, err := json.Marshal(protocol.SubagentStartedData{
		ID:        status.ID,
		ChildTurn: childTurn,
		Task:      status.Summary,
		Mode:      a.Mode,
		Index:     index + 1, // the contract is one-based; the slice is not
		Total:     len(tasks),
		// Which rung is about to run this, and why. "Four subagents, three on
		// haiku" is a different fact from "four subagents", and it is the one
		// someone watching a wide run on a subscription wants.
		Level: string(tasks[index].Level),
		Model: status.Model,
	})
	if err != nil {
		return
	}
	_, _ = a.Bus.Publish(bus.Event{
		Turn: a.lastTurnID,
		Type: protocol.EventSubagentStarted,
		Data: data,
	})
	a.notifySubagent(status)
}

// publishSubagentFinished records how one child turn ended.
//
// Published on every path out of a task, including failure: an event that only
// fires on success leaves a counter stuck at a number that never comes down,
// which is worse than no counter at all.
func (a *Agent) publishSubagentFinished(childTurn string, index int, ok bool, model, effort string) {
	a.noteSubagents(-1)
	status, changed := a.finishSubagentStatus(index, ok, model, effort)
	if changed {
		a.notifySubagent(status)
	}
	if a.Bus == nil {
		return
	}
	data, err := json.Marshal(protocol.SubagentFinishedData{
		ID:        status.ID,
		ChildTurn: childTurn,
		Mode:      a.Mode,
		OK:        ok,
		// The rung that actually ran it, which is not always the one it
		// started on: a cheaper rung that would not spawn falls back.
		Model: model,
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

// pausedChild is a child that stopped at the run's limit. It is announced as
// paused only once the pause it belongs to is durable.
type pausedChild struct {
	childTurn string
	index     int
	model     string
}

// holdSubagentPaused counts the child out at once, as every path out does,
// and says what is true now: it stopped at the limit and the pause is being
// saved. "Paused, waiting for allowance" is kept until the pause is.
func (a *Agent) holdSubagentPaused(childTurn string, index int, model string) {
	a.noteSubagents(-1)
	a.updateSubagentStatus(index, SubagentWaiting, SubagentPhaseSchedule, "stopped at the limit; saving the pause")
	a.subagentMu.Lock()
	a.pausedChildren = append(a.pausedChildren, pausedChild{childTurn: childTurn, index: index, model: model})
	a.subagentMu.Unlock()
}

// holdPausedTask keeps an unresolved task's "waiting for allowance" until the
// pause is saved.
func (a *Agent) holdPausedTask(index int) {
	a.subagentMu.Lock()
	a.pausedTasks = append(a.pausedTasks, index)
	a.subagentMu.Unlock()
}

// announcePausedChildren settles what the tasks held at a pause say: paused
// and waiting for the allowance once the pause is durable; stopped at the
// limit with no pause saved otherwise, whether its write failed or the user
// cancelled first. Nothing claims a wait that no saved pause backs.
func (a *Agent) announcePausedChildren(durable bool) {
	a.subagentMu.Lock()
	children, tasks := a.pausedChildren, a.pausedTasks
	a.pausedChildren, a.pausedTasks = nil, nil
	a.subagentMu.Unlock()
	// A held child is usually a held task too; each says it once, in order.
	held := append([]int(nil), tasks...)
	for _, child := range children {
		if !slices.Contains(held, child.index) {
			held = append(held, child.index)
		}
	}
	for _, index := range held {
		if durable {
			a.updateSubagentStatus(index, SubagentWaiting, SubagentPhaseSchedule, "paused: waiting for allowance")
		} else {
			a.updateSubagentStatus(index, SubagentFailed, SubagentPhaseComplete, "stopped at the limit; no pause was saved")
		}
	}
	for _, child := range children {
		if a.Bus == nil {
			continue
		}
		data := protocol.SubagentFinishedData{ID: a.subagentTaskID(child.index), ChildTurn: child.childTurn, Mode: a.Mode, Model: child.model}
		if durable {
			data.Reason = "paused"
		}
		encoded, _ := json.Marshal(data)
		_, _ = a.Bus.Publish(bus.Event{Turn: a.lastTurnID, Type: protocol.EventSubagentFinished, Data: encoded})
	}
}
