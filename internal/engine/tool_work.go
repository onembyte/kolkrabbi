package engine

import (
	"encoding/json"

	"github.com/onembyte/kolkrabbi/internal/bus"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/secret"
	"github.com/onembyte/kolkrabbi/protocol"
)

// toolWorkOwner identifies the work ledger a Kolkrabbi-owned tool belongs to.
// A main tool has no task coordinates; a child tool keeps both coordinates on
// every lifecycle event so concurrent event consumers do not guess ownership.
type toolWorkOwner struct {
	subagent  bool
	index     int
	taskID    string
	childTurn string
	model     string
	effort    string
}

func (a *Agent) mainToolWork(model, effort string) toolWorkOwner {
	return toolWorkOwner{model: model, effort: effort}
}

func (a *Agent) subagentToolWork(index int) toolWorkOwner {
	owner := toolWorkOwner{subagent: true, index: index}
	a.subagentMu.Lock()
	status, found := a.subagentStatus[index]
	a.subagentMu.Unlock()
	if found {
		owner.taskID = status.ID
		owner.childTurn = status.ChildTurn
		owner.model = status.Model
		owner.effort = status.Effort
	}
	return owner
}

func (a *Agent) publishKolkToolRequested(call provider.ToolCall, owner toolWorkOwner) {
	a.publishKolkToolEvent(protocol.EventToolRequested, kolkToolRequestedData(call, owner))
}

// kolkToolRequestedData makes the durable request frame from provider-owned
// arguments without letting those arguments bypass the same scrubber that
// protects tool output and status text.
func kolkToolRequestedData(call provider.ToolCall, owner toolWorkOwner) protocol.ToolRequestedData {
	return protocol.ToolRequestedData{
		ID: call.ID, Name: secret.Scrub(call.Function.Name), Arguments: secret.Scrub(call.Function.Arguments),
		Executor: protocol.ToolExecutorKolk, TaskID: owner.taskID, ChildTurn: owner.childTurn,
	}
}

func (a *Agent) publishKolkToolStarted(call provider.ToolCall, owner toolWorkOwner) {
	a.publishKolkToolEvent(protocol.EventToolStarted, protocol.ToolStartedData{
		ID: call.ID, Executor: protocol.ToolExecutorKolk, TaskID: owner.taskID, ChildTurn: owner.childTurn,
	})
	a.updateKolkToolWork(owner, "running "+describeToolCall(call))
}

func (a *Agent) publishKolkToolOutput(call provider.ToolCall, result string, owner toolWorkOwner) {
	a.publishKolkToolEvent(protocol.EventToolOutput, protocol.ToolOutputData{
		ID: call.ID, Output: secret.Scrub(result), Executor: protocol.ToolExecutorKolk,
		TaskID: owner.taskID, ChildTurn: owner.childTurn,
	})
}

// publishKolkToolSkipped records a requested call that the doom-loop guard
// refused before it reached the executor. It intentionally emits no
// tool.started or tool.finished event: those describe a run that never began.
func (a *Agent) publishKolkToolSkipped(call provider.ToolCall, owner toolWorkOwner) {
	a.updateKolkToolWork(owner, "skipped "+compactToolText(secret.Scrub(call.Function.Name))+": repeated call")
}

func (a *Agent) publishKolkToolFinished(call provider.ToolCall, err error, owner toolWorkOwner) {
	a.publishKolkToolEvent(protocol.EventToolFinished, protocol.ToolFinishedData{
		ID: call.ID, OK: err == nil, Executor: protocol.ToolExecutorKolk,
		TaskID: owner.taskID, ChildTurn: owner.childTurn,
	})
	step := "finished " + compactToolText(secret.Scrub(call.Function.Name))
	if err != nil {
		step = "failed " + compactToolText(secret.Scrub(call.Function.Name)) + ": " + compactSubagentStep(secret.Scrub(err.Error()))
	}
	a.updateKolkToolWork(owner, step)
}

func (a *Agent) publishKolkToolEvent(event protocol.EventType, data any) {
	if a.Bus == nil {
		return
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	_, _ = a.Bus.Publish(bus.Event{Turn: a.lastTurnID, Type: event, Data: raw})
}

func (a *Agent) updateKolkToolWork(owner toolWorkOwner, step string) {
	if owner.subagent {
		a.updateSubagentStatus(owner.index, SubagentWorking, SubagentPhaseTool, step)
		return
	}
	if a.Mode == ModeAgent {
		a.publishMainWork(protocol.WorkStateWorking, protocol.WorkPhaseTool, step, owner.model, owner.effort)
	}
}
