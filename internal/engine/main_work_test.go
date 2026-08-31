package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/bus"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/protocol"
)

func mainWorkEvents(t *testing.T, b *bus.Bus) []protocol.WorkUpdatedData {
	t.Helper()
	sub, err := b.Subscribe(0)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	var found []protocol.WorkUpdatedData
	for _, envelope := range sub.Replay() {
		if envelope.Type != protocol.EventWorkUpdated {
			continue
		}
		var data protocol.WorkUpdatedData
		if err := json.Unmarshal(envelope.Data, &data); err != nil {
			t.Fatal(err)
		}
		if data.Role == protocol.WorkRoleMain {
			found = append(found, data)
		}
	}
	return found
}

func TestOrchestratedRunPublishesOrderedMainWorkPhases(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{Text: `[{"title":"read one","kind":"research"},{"title":"read two","kind":"research"}]`},
		enginetest.Step{Text: "one"}, enginetest.Step{Text: "two"}, enginetest.Step{Text: "answer"},
	)
	defer srv.Close()
	a, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	b := newTestBus(t)
	a.Bus = b

	if err := a.RunTurn(context.Background(), "review it"); err != nil {
		t.Fatal(err)
	}
	events := mainWorkEvents(t, b)
	if len(events) != 4 {
		t.Fatalf("main work updates = %+v, want planning, delegation, synthesis and completion", events)
	}
	want := []protocol.WorkPhase{
		protocol.WorkPhasePlanning, protocol.WorkPhaseSchedule,
		protocol.WorkPhaseSynthesis, protocol.WorkPhaseComplete,
	}
	for index, event := range events {
		if event.ID != a.lastTurnID || event.Sequence != uint64(index+1) || event.Phase != want[index] {
			t.Fatalf("main work[%d] = %+v, want turn %q sequence %d phase %q", index, event,
				a.lastTurnID, index+1, want[index])
		}
		if event.ChildTurn != "" || event.Index != 0 || event.Total != 0 || strings.TrimSpace(event.Step) == "" {
			t.Fatalf("main work leaked child correlation or lost its step: %+v", event)
		}
	}
	if events[3].State != protocol.WorkStateDone {
		t.Fatalf("final main work = %+v, want done", events[3])
	}
}

func TestOrchestratedPlanningFailureTerminatesMainWork(t *testing.T) {
	srv := enginetest.New(enginetest.Step{StatusCode: 400, ErrorBody: `{"error":{"message":"planner refused"}}`})
	defer srv.Close()
	a, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	b := newTestBus(t)
	a.Bus = b

	if err := a.RunTurn(context.Background(), "review it"); err == nil {
		t.Fatal("RunTurn succeeded despite planner refusal")
	}
	events := mainWorkEvents(t, b)
	if len(events) != 2 || events[0].Phase != protocol.WorkPhasePlanning ||
		events[1].State != protocol.WorkStateFailed || events[1].Phase != protocol.WorkPhaseComplete {
		t.Fatalf("main failure work = %+v", events)
	}
}

func TestMainWorkSequenceRestartsForANewTurn(t *testing.T) {
	b := newTestBus(t)
	a := &Agent{Options: Options{Bus: b, Mode: ModeAgent, Effort: EffortMedium}}
	a.lastTurnID = "t_01ARYZ6S41TSV4RRFFQ69G5FAW"
	a.resetMainWork()
	a.publishMainWork(protocol.WorkStateWorking, protocol.WorkPhasePlanning, "first plan", "gpt-5.6-luna", EffortMedium)
	a.lastTurnID = "t_01ARYZ6S41TSV4RRFFQ69G5FAY"
	a.resetMainWork()
	a.publishMainWork(protocol.WorkStateWorking, protocol.WorkPhasePlanning, "second plan", "gpt-5.6-luna", EffortMedium)

	events := mainWorkEvents(t, b)
	if len(events) != 2 || events[0].ID == events[1].ID || events[0].Sequence != 1 || events[1].Sequence != 1 {
		t.Fatalf("per-turn work sequences = %+v, want distinct turn ids and 1,1", events)
	}
}
