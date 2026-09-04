package engine

import (
	"context"
	"errors"
	"testing"
)

// plannerSpy hands out chapter titles in order and records what it was told.
type plannerSpy struct {
	titles []string
	seen   [][]Chapter
	err    error
	calls  int
}

func (p *plannerSpy) Next(_ context.Context, _ string, done []Chapter) (string, error) {
	p.calls++
	p.seen = append(p.seen, append([]Chapter(nil), done...))
	if p.err != nil {
		return "", p.err
	}
	if p.calls > len(p.titles) {
		return "", nil
	}
	return p.titles[p.calls-1], nil
}

func plannedRunner(planner ChapterPlanner, worker ChapterWorker) *SagaRunner {
	executor := executorFor(worker, dirtyRunner())
	executor.Planner = planner
	executor.Budget = SagaBudget{MaxChapters: 10, CostLimit: 100}
	return executor
}

func TestAGoalWithNoChaptersPlansOne(t *testing.T) {
	planner := &plannerSpy{titles: []string{"audit the database package"}}
	worker := &workerSpy{}
	state := &SagaState{Goal: "migrate the store"}

	reason, err := wakes(t, plannedRunner(planner, worker), state)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The doc's napkin test starts from a goal and nothing else.
	if len(state.Chapters) != 1 || state.Chapters[0].Title != "audit the database package" {
		t.Fatalf("chapters = %+v", state.Chapters)
	}
	if state.Chapters[0].Number != 1 {
		t.Fatalf("first chapter numbered %d", state.Chapters[0].Number)
	}
	if len(worker.worked) != 1 {
		t.Fatalf("worked %v", worker.worked)
	}
	if reason != StopGoalComplete {
		t.Fatalf("reason = %q, want goal-complete once the planner has nothing left", reason)
	}
}

func TestThePlannerSeesWhatIsAlreadyDone(t *testing.T) {
	planner := &plannerSpy{titles: []string{"first", "second"}}
	state := &SagaState{Goal: "g"}

	if _, err := wakes(t, plannedRunner(planner, &workerSpy{}), state); err != nil {
		t.Fatal(err)
	}

	if planner.calls < 2 {
		t.Fatalf("planner called %d times", planner.calls)
	}
	// Choosing "one discrete task that moves closer to the goal" is impossible
	// without knowing what the previous chapters achieved.
	second := planner.seen[1]
	if len(second) != 1 || second[0].Title != "first" || second[0].Status != StatusDone {
		t.Fatalf("the planner's second call saw %+v", second)
	}
}

func TestChaptersAreNumberedInSequence(t *testing.T) {
	planner := &plannerSpy{titles: []string{"a", "b", "c"}}
	state := &SagaState{Goal: "g"}

	if _, err := wakes(t, plannedRunner(planner, &workerSpy{}), state); err != nil {
		t.Fatal(err)
	}

	for i, chapter := range state.Chapters {
		if chapter.Number != i+1 {
			t.Fatalf("chapter %d is numbered %d", i, chapter.Number)
		}
	}
}

func TestAPlannerThatFailsStopsTheRun(t *testing.T) {
	planner := &plannerSpy{err: errors.New("the model refused")}
	worker := &workerSpy{}

	_, err := wakes(t, plannedRunner(planner, worker), &SagaState{Goal: "g"})

	// A planner that errors must not become a loop that asks forever.
	if err == nil {
		t.Fatal("a failing planner reported success")
	}
	if len(worker.worked) != 0 {
		t.Fatalf("worked %v without a chapter to work", worker.worked)
	}
}

func TestWithoutAPlannerHandWrittenChaptersStillWork(t *testing.T) {
	worker := &workerSpy{}
	state := oneChapter(StatusPending)
	executor := executorFor(worker, dirtyRunner()) // no Planner

	reason, err := wakes(t, executor, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(worker.worked) != 1 {
		t.Fatalf("worked %v", worker.worked)
	}
	// Without a planner there is nothing to ask for more, so running out of
	// chapters is running out of work — not a completed goal.
	if reason != StopNoWork {
		t.Fatalf("reason = %q, want no-work", reason)
	}
}

func TestPlanningStopsAtTheChapterCeiling(t *testing.T) {
	planner := &plannerSpy{titles: []string{"a", "b", "c", "d", "e"}}
	worker := &workerSpy{}
	executor := plannedRunner(planner, worker)
	executor.Budget = SagaBudget{MaxChapters: 2, CostLimit: 100}

	reason, _ := wakes(t, executor, &SagaState{Goal: "g"})

	// A planner that can always think of something else would otherwise run
	// until the money ran out.
	if reason != StopMaxChapters {
		t.Fatalf("reason = %q, want max-chapters", reason)
	}
	if len(worker.worked) > 2 {
		t.Fatalf("worked %d chapters past a ceiling of 2", len(worker.worked))
	}
}
