package engine

import (
	"context"
	"sync"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
)

// The count is the whole point of A33.1's events: how wide did this run go, and
// is it still going. It rises as subagents start and returns to zero when the
// run ends — including when tasks fail, which is the case that would otherwise
// leave a number on screen forever.
func TestTheAgentCountRisesAndReturnsToZero(t *testing.T) {
	var mu sync.Mutex
	var seen []int
	a := &Agent{Options: Options{Mode: ModeAgent, Agents: func(running int) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, running)
	}}}
	a.lastTurnID = "t_01ARYZ6S41TSV4RRFFQ69G5FAW"
	tasks := []Task{{Title: "one"}, {Title: "two"}}

	a.publishSubagentStarted(tasks, 0, "t_01ARYZ6S41TSV4RRFFQ69G5FAX")
	a.publishSubagentStarted(tasks, 1, "t_01ARYZ6S41TSV4RRFFQ69G5FAY")
	a.publishSubagentFinished("t_01ARYZ6S41TSV4RRFFQ69G5FAX", 0, true)
	a.publishSubagentFinished("t_01ARYZ6S41TSV4RRFFQ69G5FAY", 1, false)

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 4 {
		t.Fatalf("the port was told %d times, want once per start and finish: %v", len(seen), seen)
	}
	if seen[0] != 1 || seen[1] != 2 {
		t.Errorf("count did not rise with each start: %v", seen)
	}
	if seen[3] != 0 {
		t.Errorf("count ended at %d, want zero — a failed task must still come off the count: %v", seen[3], seen)
	}
}

// runOneTask runs in a goroutine per task, so the counter is written from
// several at once.
func TestTheAgentCountIsSafeUnderConcurrency(t *testing.T) {
	a := &Agent{Options: Options{Mode: ModeAgent, Agents: func(int) {}}}
	a.lastTurnID = "t_01ARYZ6S41TSV4RRFFQ69G5FAW"
	tasks := make([]Task, 32)
	for i := range tasks {
		tasks[i] = Task{Title: "t"}
	}

	var wg sync.WaitGroup
	for i := range tasks {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			a.publishSubagentStarted(tasks, index, "t_01ARYZ6S41TSV4RRFFQ69G5FAX")
			a.publishSubagentFinished("t_01ARYZ6S41TSV4RRFFQ69G5FAX", index, true)
		}(i)
	}
	wg.Wait()

	if running := a.RunningSubagents(); running != 0 {
		t.Errorf("after every task finished the count is %d, want zero", running)
	}
}

// A real run, because a counter nothing increments is a counter that reads zero
// forever and looks correct.
func TestARealRunMovesTheCount(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{Text: `{"tasks":[{"title":"read","kind":"research"},{"title":"explain","kind":"explain"}]}`},
		enginetest.Step{Text: "read it"},
		enginetest.Step{Text: "explained"},
		enginetest.Step{Text: "done"},
	)
	defer srv.Close()

	var mu sync.Mutex
	highest := 0
	ag, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	ag.Agents = func(running int) {
		mu.Lock()
		defer mu.Unlock()
		if running > highest {
			highest = running
		}
	}

	_ = ag.RunTurn(context.Background(), "two things")

	mu.Lock()
	defer mu.Unlock()
	if highest == 0 {
		t.Fatal("an orchestrated run never reported a running subagent")
	}
	if running := ag.RunningSubagents(); running != 0 {
		t.Errorf("the run ended with %d agents still counted", running)
	}
}

// A session with no observer must behave exactly as before.
func TestTheCountIsSilentWithoutAnObserver(t *testing.T) {
	a := &Agent{Options: Options{Mode: ModeAgent}}
	a.lastTurnID = "t_01ARYZ6S41TSV4RRFFQ69G5FAW"
	a.publishSubagentStarted([]Task{{Title: "one"}}, 0, "t_01ARYZ6S41TSV4RRFFQ69G5FAX")
	if running := a.RunningSubagents(); running != 1 {
		t.Errorf("the count is %d without an observer, want it kept anyway", running)
	}
}
