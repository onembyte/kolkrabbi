package engine

import (
	"encoding/json"

	"github.com/onembyte/kolkrabbi/internal/bus"
	"github.com/onembyte/kolkrabbi/protocol"
)

func (a *Agent) resetMainWork() {
	a.mainWorkMu.Lock()
	a.mainWorkTurn = a.lastTurnID
	a.mainWorkSequence = 0
	a.mainWorkMu.Unlock()
}

// publishMainWork records one observed parent-turn boundary. It is deliberately
// separate from the subagent status callback: the main agent has no task row
// to replace, but its planning/delegation/synthesis decisions must survive in
// the same durable journal as child work.
func (a *Agent) publishMainWork(state protocol.WorkState, phase protocol.WorkPhase, step, model, effort string) {
	if a.Bus == nil || a.lastTurnID == "" {
		return
	}
	step = compactSubagentStep(step)
	if step == "" {
		return
	}

	a.mainWorkMu.Lock()
	defer a.mainWorkMu.Unlock()
	if a.mainWorkTurn != a.lastTurnID {
		a.mainWorkTurn = a.lastTurnID
		a.mainWorkSequence = 0
	}
	sequence := a.mainWorkSequence + 1
	data, err := json.Marshal(protocol.WorkUpdatedData{
		ID:       a.lastTurnID,
		Role:     protocol.WorkRoleMain,
		State:    state,
		Phase:    phase,
		Step:     step,
		Sequence: sequence,
		Model:    model,
		Effort:   effort,
	})
	if err != nil {
		return
	}
	if _, err := a.Bus.Publish(bus.Event{
		Turn: a.lastTurnID,
		Type: protocol.EventWorkUpdated,
		Data: data,
	}); err == nil {
		a.mainWorkSequence = sequence
	}
}
