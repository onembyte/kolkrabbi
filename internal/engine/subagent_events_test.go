package engine

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/bus"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
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

func workEvents(t *testing.T, b *bus.Bus) []protocol.Envelope {
	t.Helper()
	sub, err := b.Subscribe(0)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	var found []protocol.Envelope
	for _, envelope := range sub.Replay() {
		if envelope.Type == protocol.EventWorkUpdated {
			found = append(found, envelope)
		}
	}
	return found
}

func TestSubagentLifecyclePublishesEveryStatusAsOrderedWork(t *testing.T) {
	b := newTestBus(t)
	a := &Agent{Options: Options{Bus: b, Mode: ModeAgent}}
	a.lastTurnID = "t_01ARYZ6S41TSV4RRFFQ69G5FAW"
	tasks := []Task{{Title: "inspect the runtime", Kind: KindResearch}}
	child := "t_01ARYZ6S41TSV4RRFFQ69G5FAX"

	a.publishSubagentStarted(tasks, 0, child, "gpt-5.6-luna", EffortLow)
	a.updateSubagentStatusRoute(0, "gpt-5.6-sol", EffortMedium)
	a.publishSubagentFinished(child, 0, true, "gpt-5.6-sol", EffortMedium)

	events := workEvents(t, b)
	if len(events) != 3 {
		t.Fatalf("published %d work updates, want start, fallback and finish", len(events))
	}
	for index, envelope := range events {
		var data protocol.WorkUpdatedData
		if err := json.Unmarshal(envelope.Data, &data); err != nil {
			t.Fatal(err)
		}
		if data.Sequence != uint64(index+1) {
			t.Fatalf("work sequence[%d] = %d, want %d", index, data.Sequence, index+1)
		}
		if data.ChildTurn != child || data.Index != 1 || data.Total != 1 {
			t.Fatalf("work correlation[%d] = %+v", index, data)
		}
	}
	var terminal protocol.WorkUpdatedData
	if err := json.Unmarshal(events[2].Data, &terminal); err != nil {
		t.Fatal(err)
	}
	if terminal.State != protocol.WorkStateDone || terminal.Phase != protocol.WorkPhaseComplete {
		t.Fatalf("terminal work = %+v", terminal)
	}
}

func TestConcurrentSubagentWorkUpdatesStayGloballyOrdered(t *testing.T) {
	const count = 24
	b := newTestBus(t)
	a := &Agent{Options: Options{Bus: b, Mode: ModeAgent}}
	a.lastTurnID = "t_01ARYZ6S41TSV4RRFFQ69G5FAW"
	tasks := make([]Task, count)
	for index := range tasks {
		tasks[index] = Task{Title: "task " + itoa(index+1), Kind: KindResearch}
	}

	var wg sync.WaitGroup
	for index := range tasks {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			a.publishSubagentStarted(tasks, index, "t_01ARYZ6S41TSV4RRFFQ69G5FAX", "gpt-5.6-luna", EffortLow)
		}(index)
	}
	wg.Wait()

	events := workEvents(t, b)
	if len(events) != count {
		t.Fatalf("published %d work updates, want %d", len(events), count)
	}
	seen := make(map[string]bool, count)
	var previous uint64
	for _, envelope := range events {
		if envelope.Seq <= previous {
			t.Fatalf("global journal sequence moved backward: %d after %d", envelope.Seq, previous)
		}
		previous = envelope.Seq
		var data protocol.WorkUpdatedData
		if err := json.Unmarshal(envelope.Data, &data); err != nil {
			t.Fatal(err)
		}
		if data.Sequence != 1 {
			t.Fatalf("first task update sequence = %d, want 1", data.Sequence)
		}
		if seen[data.ID] {
			t.Fatalf("duplicate task id %s", data.ID)
		}
		seen[data.ID] = true
	}
}

func TestWorkUpdatesRecoverFromTheDurableJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.events.ndjson")
	b, err := bus.New("s_01ARYZ6S41TSV4RRFFQ69G5FAV", bus.Options{SpillPath: path})
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{Options: Options{Bus: b, Mode: ModeAgent}}
	a.lastTurnID = "t_01ARYZ6S41TSV4RRFFQ69G5FAW"
	child := "t_01ARYZ6S41TSV4RRFFQ69G5FAX"
	a.publishSubagentStarted([]Task{{Title: "durable task", Kind: KindResearch}}, 0, child,
		"gpt-5.6-luna", EffortMedium)
	a.publishSubagentFinished(child, 0, true, "gpt-5.6-luna", EffortMedium)
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}

	recovered, err := bus.New("s_01ARYZ6S41TSV4RRFFQ69G5FAV", bus.Options{SpillPath: path})
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	events := workEvents(t, recovered)
	if len(events) != 2 {
		t.Fatalf("recovered %d work updates, want start and finish", len(events))
	}
	if events[0].Seq >= events[1].Seq {
		t.Fatalf("recovered sequence = %d then %d", events[0].Seq, events[1].Seq)
	}
}

// A work ledger is useful only if a consumer can reconnect after a busy,
// concurrent run. Keep just one event in memory so this exercises the spilled
// journal, and leave a live reader unread so a future blocking fan-out cannot
// hide behind a unit test that always drains promptly.
func TestConcurrentTaskWorkSurvivesSpillReopenAndSlowSubscriber(t *testing.T) {
	const session = "s_01ARYZ6S41TSV4RRFFQ69G5FAV"
	path := filepath.Join(t.TempDir(), "session.events.ndjson")
	b, err := bus.New(session, bus.Options{
		MaxEvents:        1,
		SubscriberBuffer: 1,
		SpillPath:        path,
	})
	if err != nil {
		t.Fatal(err)
	}

	// This subscriber intentionally never reads while work is published. The
	// journal must disconnect it rather than make either concurrent task wait.
	slow, err := b.Subscribe(0)
	if err != nil {
		t.Fatal(err)
	}

	a := &Agent{Options: Options{
		Mode: ModeAgent, Out: io.Discard, Permission: PermissionFullAuto, Bus: b,
		SubagentBackend: func(_ context.Context, model, _ string, _ string) (ChatBackend, error) {
			return observedWorkBackend{events: []provider.ProgressEvent{{
				Kind: provider.ProgressMessage, Detail: "thinking",
			}}, text: model + " done"}, nil
		},
	}}
	a.lastTurnID = "t_01ARYZ6S41TSV4RRFFQ69G5FAW"
	a.MaxConcurrentTasks = 2

	completed := make(chan error, 1)
	go func() {
		_, runErr := a.runTasks(context.Background(), "inspect both", []Task{
			{Title: "first", Kind: KindResearch, Model: "provider/first"},
			{Title: "second", Kind: KindResearch, Model: "provider/second"},
		})
		completed <- runErr
	}()
	select {
	case err := <-completed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent work publication blocked behind an unread subscriber")
	}

	// Drain after publication only. The one buffered envelope is not enough for
	// this run, so a healthy bus closes the subscriber with its replay cursor.
	for range slow.Events() {
	}
	if !errors.Is(slow.Err(), bus.ErrSlowSubscriber) {
		t.Fatalf("slow subscriber error = %v, want %v", slow.Err(), bus.ErrSlowSubscriber)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}

	recovered, err := bus.New(session, bus.Options{MaxEvents: 1, SpillPath: path})
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()

	byTask := make(map[string][]protocol.WorkUpdatedData)
	var previousEnvelope uint64
	for _, envelope := range workEvents(t, recovered) {
		if envelope.Seq <= previousEnvelope {
			t.Fatalf("recovered journal sequence moved backward: %d after %d", envelope.Seq, previousEnvelope)
		}
		previousEnvelope = envelope.Seq
		var work protocol.WorkUpdatedData
		if err := json.Unmarshal(envelope.Data, &work); err != nil {
			t.Fatal(err)
		}
		byTask[work.ID] = append(byTask[work.ID], work)
	}
	if len(byTask) != 2 {
		t.Fatalf("recovered work ledgers = %+v, want two task identities", byTask)
	}
	for id, updates := range byTask {
		if len(updates) != 5 {
			t.Fatalf("task %s recovered %d updates, want queue/start/open/provider/done: %+v", id, len(updates), updates)
		}
		first := updates[0]
		for index, update := range updates {
			if update.Role != protocol.WorkRoleSubagent || update.ID != first.ID ||
				update.ChildTurn != first.ChildTurn || update.Index != first.Index || update.Total != 2 ||
				update.Sequence != uint64(index+1) {
				t.Fatalf("task %s recovered corrupt update[%d]: %+v", id, index, update)
			}
		}
		last := updates[len(updates)-1]
		if last.State != protocol.WorkStateDone || last.Phase != protocol.WorkPhaseComplete {
			t.Fatalf("task %s terminal recovery = %+v", id, last)
		}
	}
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

	a.publishSubagentStarted(tasks, 0, "t_01ARYZ6S41TSV4RRFFQ69G5FAX", "", EffortMedium)
	a.publishSubagentFinished("t_01ARYZ6S41TSV4RRFFQ69G5FAX", 0, true, "", EffortMedium)

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

	a.publishSubagentStarted(tasks, 0, "t_01ARYZ6S41TSV4RRFFQ69G5FAX", "", EffortMedium)
	a.publishSubagentFinished("t_01ARYZ6S41TSV4RRFFQ69G5FAX", 0, false, "", EffortMedium)

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
	a.publishSubagentStarted([]Task{{Title: "one"}}, 0, "t_01ARYZ6S41TSV4RRFFQ69G5FAX", "", EffortMedium)
	a.publishSubagentFinished("t_01ARYZ6S41TSV4RRFFQ69G5FAX", 0, true, "", EffortMedium)
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
