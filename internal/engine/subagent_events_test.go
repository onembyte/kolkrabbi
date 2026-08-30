package engine

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/bus"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/protocol"
)

// subagentEvents drains a bus and returns the subagent lifecycle events in order.
func subagentEvents(t *testing.T, b *bus.Bus) []protocol.Envelope {
	t.Helper()
	sub, err := b.Subscribe(0)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()

	var found []protocol.Envelope
	for _, envelope := range sub.Replay() {
		switch envelope.Type {
		case protocol.EventSubagentStarted, protocol.EventSubagentFinished:
			found = append(found, envelope)
		}
	}
	return found
}

// The protocol has defined subagent.started and subagent.finished since A7, and
// nothing has ever published them: the count of running agents cannot leave the
// engine, so nothing can show it. This is that publisher.
func TestAnOrchestratedRunPublishesOneStartAndOneFinishPerTask(t *testing.T) {
	tasks := []Task{
		{Title: "read the config", Kind: KindResearch},
		{Title: "explain it", Kind: KindExplain},
	}
	b := newTestBus(t)
	a := &Agent{Options: Options{Bus: b, Mode: ModeAgent}}
	a.lastTurnID = "t_01ARYZ6S41TSV4RRFFQ69G5FAW"

	a.publishSubagentStarted(tasks, 0, "t_01ARYZ6S41TSV4RRFFQ69G5FAX")
	a.publishSubagentFinished("t_01ARYZ6S41TSV4RRFFQ69G5FAX", 0, true, "")

	events := subagentEvents(t, b)
	if len(events) != 2 {
		t.Fatalf("published %d subagent events, want a start and a finish", len(events))
	}
	if events[0].Type != protocol.EventSubagentStarted || events[1].Type != protocol.EventSubagentFinished {
		t.Fatalf("events out of order: %v then %v", events[0].Type, events[1].Type)
	}

	var started protocol.SubagentStartedData
	if err := json.Unmarshal(events[0].Data, &started); err != nil {
		t.Fatal(err)
	}
	if started.Task != "read the config" {
		t.Errorf("task = %q, want the planner's title", started.Task)
	}
	if started.Index != 1 || started.Total != 2 {
		t.Errorf("index/total = %d/%d, want 1/2 — one-based, as the contract requires", started.Index, started.Total)
	}
	if started.Mode == "" {
		t.Error("mode is empty, which the contract refuses")
	}
}

// The two events must correlate, or a reader cannot pair them and a count never
// comes back down.
func TestAStartAndItsFinishShareIdentifiers(t *testing.T) {
	b := newTestBus(t)
	a := &Agent{Options: Options{Bus: b, Mode: ModeAgent}}
	a.lastTurnID = "t_01ARYZ6S41TSV4RRFFQ69G5FAW"
	tasks := []Task{{Title: "one", Kind: KindEdit}}

	a.publishSubagentStarted(tasks, 0, "t_01ARYZ6S41TSV4RRFFQ69G5FAX")
	a.publishSubagentFinished("t_01ARYZ6S41TSV4RRFFQ69G5FAX", 0, false, "")

	events := subagentEvents(t, b)
	if len(events) != 2 {
		t.Fatalf("published %d events, want two", len(events))
	}
	var started protocol.SubagentStartedData
	var finished protocol.SubagentFinishedData
	if err := json.Unmarshal(events[0].Data, &started); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(events[1].Data, &finished); err != nil {
		t.Fatal(err)
	}
	if started.ID != finished.ID {
		t.Errorf("ids differ: %q started, %q finished", started.ID, finished.ID)
	}
	if started.ChildTurn != finished.ChildTurn {
		t.Errorf("child turns differ: %q then %q", started.ChildTurn, finished.ChildTurn)
	}
	if finished.OK {
		t.Error("a failed task reported ok")
	}
}

// The same task index must yield the same task id across both events, and two
// tasks must not collide.
func TestTaskIdentifiersAreStableAndDistinct(t *testing.T) {
	b := newTestBus(t)
	a := &Agent{Options: Options{Bus: b, Mode: ModeAgent}}
	a.lastTurnID = "t_01ARYZ6S41TSV4RRFFQ69G5FAW"

	first := a.subagentTaskID(0)
	if first != a.subagentTaskID(0) {
		t.Error("the same task index produced two different ids, so a reader cannot pair the events")
	}
	if first == a.subagentTaskID(1) {
		t.Error("two tasks share one id")
	}
}

// A run with no bus attached must behave exactly as before.
func TestPublishingIsSilentWithoutABus(t *testing.T) {
	a := &Agent{Options: Options{Mode: ModeAgent}}
	a.publishSubagentStarted([]Task{{Title: "one"}}, 0, "t_01ARYZ6S41TSV4RRFFQ69G5FAX")
	a.publishSubagentFinished("t_01ARYZ6S41TSV4RRFFQ69G5FAX", 0, true, "")
}

func newTestBus(t *testing.T) *bus.Bus {
	t.Helper()
	b, err := bus.New("s_01ARYZ6S41TSV4RRFFQ69G5FAV", bus.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}

// The publisher above is only worth having if the orchestrator calls it. This
// drives a real orchestrated run against the mock and reads the events off the
// bus, which is the difference between a function that works and a feature that
// exists.
func TestARealOrchestratedRunEmitsThePairs(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{Text: `{"tasks":[{"title":"read the config","kind":"research"},{"title":"explain it","kind":"explain"}]}`},
		enginetest.Step{Text: "config read"},
		enginetest.Step{Text: "explained"},
		enginetest.Step{Text: "done"},
	)
	defer srv.Close()

	ag, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	b := newTestBus(t)
	ag.Bus = b

	_ = ag.RunTurn(context.Background(), "do two things")

	var started, finished int
	for _, envelope := range subagentEvents(t, b) {
		switch envelope.Type {
		case protocol.EventSubagentStarted:
			started++
		case protocol.EventSubagentFinished:
			finished++
		}
	}
	if started == 0 {
		t.Fatal("an orchestrated run published no subagent.started, so nothing can count agents")
	}
	if started != finished {
		t.Errorf("%d started and %d finished: a count built on this would not return to zero",
			started, finished)
	}
}

// The pairing above passes even if the finish is only published on success,
// because every task in that fixture succeeds — the property was vacuously
// covered. This is the run where a subagent fails: the provider has no step
// left for it, so its turn errors.
//
// A count built on events that only fire on success would stick at one forever.
func TestAFailedSubagentStillPublishesItsFinish(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{Text: `{"tasks":[{"title":"first","kind":"research"},{"title":"second","kind":"research"}]}`},
		enginetest.Step{Text: "the only subagent answer there is"},
	)
	defer srv.Close()

	ag, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	b := newTestBus(t)
	ag.Bus = b

	_ = ag.RunTurn(context.Background(), "two tasks, one answer")

	var started, finished, failed int
	for _, envelope := range subagentEvents(t, b) {
		switch envelope.Type {
		case protocol.EventSubagentStarted:
			started++
		case protocol.EventSubagentFinished:
			finished++
			var data protocol.SubagentFinishedData
			if err := json.Unmarshal(envelope.Data, &data); err == nil && !data.OK {
				failed++
			}
		}
	}
	if started < 2 {
		t.Fatalf("started %d subagents, want the two the planner asked for", started)
	}
	if started != finished {
		t.Fatalf("%d started and %d finished — a failing task did not publish its finish", started, finished)
	}
	if failed == 0 {
		t.Error("no subagent reported failure, so this fixture is not exercising the failure path")
	}
}
