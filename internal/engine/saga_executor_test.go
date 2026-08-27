package engine

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// workerSpy stands in for whatever actually does a chapter's work.
type workerSpy struct {
	worked  []string
	cost    float64
	summary string
	err     error
}

func (w *workerSpy) Work(_ context.Context, chapter Chapter, _ string) (WorkResult, error) {
	w.worked = append(w.worked, chapter.Title)
	if w.err != nil {
		return WorkResult{}, w.err
	}
	return WorkResult{CostUSD: w.cost, Summary: w.summary}, nil
}

// noGates is a detector that finds nothing, so these tests exercise the
// executor rather than gate discovery. gates_test.go already covers detection.
type noGates struct{}

func (noGates) Detect(string) []QualityGate { return nil }

func dirtyRunner() *scriptedRunner {
	return &scriptedRunner{replies: map[string]CommandResult{
		"git status --porcelain":     {Output: " M main.go\n"},
		"git rev-parse --short HEAD": {Output: "abc1234"},
	}}
}

func executorFor(worker ChapterWorker, runner CommandRunner) *SagaRunner {
	return &SagaRunner{
		Worker:   worker,
		Runner:   runner,
		Detector: noGates{},
		Write:    func(string, []byte, os.FileMode) error { return nil },
		Now:      func() time.Time { return time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC) },
	}
}

func oneChapter(status ChapterStatus) *SagaState {
	return &SagaState{
		Goal:     "make it work",
		Chapters: []Chapter{{Number: 1, Title: "first", Status: status}},
	}
}

func TestAChapterIsWorkedThenVerified(t *testing.T) {
	worker := &workerSpy{cost: 0.25, summary: "did the thing"}
	state := oneChapter(StatusPending)

	if err := executorFor(worker, dirtyRunner()).RunChapter(context.Background(), "/repo", state, 0); err != nil {
		t.Fatalf("RunChapter: %v", err)
	}

	if len(worker.worked) != 1 || worker.worked[0] != "first" {
		t.Fatalf("worker saw %v", worker.worked)
	}
	chapter := state.Chapters[0]
	if chapter.Status != StatusDone {
		t.Fatalf("status = %q, want completed", chapter.Status)
	}
	if chapter.Commit != "abc1234" {
		t.Fatalf("commit = %q", chapter.Commit)
	}
	// Cost has to land on the chapter and in the running total, or the budget
	// guard is measuring nothing.
	if chapter.CostUSD != 0.25 || state.CumulativeCost != 0.25 {
		t.Fatalf("cost: chapter %v, cumulative %v", chapter.CostUSD, state.CumulativeCost)
	}
}

func TestAChapterThatCannotBeWorkedFailsWithoutVerifying(t *testing.T) {
	worker := &workerSpy{err: errors.New("the model gave up")}
	runner := dirtyRunner()
	state := oneChapter(StatusPending)

	err := executorFor(worker, runner).RunChapter(context.Background(), "/repo", state, 0)

	if err == nil {
		t.Fatal("a failed chapter reported success")
	}
	if state.Chapters[0].Status != StatusFailed {
		t.Fatalf("status = %q, want failed", state.Chapters[0].Status)
	}
	// Verifying work that was never done would commit whatever happened to be
	// in the tree and call it the chapter.
	for _, asked := range runner.asked {
		if strings.HasPrefix(asked, "git add") {
			t.Fatalf("a failed chapter committed: %v", runner.asked)
		}
	}
}

func TestTheArtifactIsWrittenAfterEveryChapter(t *testing.T) {
	var written int
	executor := executorFor(&workerSpy{}, dirtyRunner())
	executor.Write = func(string, []byte, os.FileMode) error { written++; return nil }

	_ = executor.RunChapter(context.Background(), "/repo", oneChapter(StatusPending), 0)

	// A saga that crashes without persisting has done work nobody can resume.
	if written == 0 {
		t.Fatal("the chapter finished without persisting SAGA.md")
	}
}

func TestTheRunStopsAtTheChapterCeiling(t *testing.T) {
	worker := &workerSpy{}
	state := &SagaState{Goal: "g", MaxChapters: 2}
	for i := 1; i <= 3; i++ {
		state.Chapters = append(state.Chapters, Chapter{Number: i, Title: "c", Status: StatusPending})
	}
	executor := executorFor(worker, dirtyRunner())
	executor.Budget = SagaBudget{MaxChapters: 2}

	reason, err := executor.Run(context.Background(), "/repo", state)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if reason != StopMaxChapters {
		t.Fatalf("reason = %q, want max-chapters", reason)
	}
	if len(worker.worked) != 0 {
		t.Fatalf("worked %v despite already being at the ceiling", worker.worked)
	}
}

func TestTheRunStopsWhenTheMoneyRunsOut(t *testing.T) {
	worker := &workerSpy{cost: 0.60}
	state := &SagaState{Goal: "g"}
	for i := 1; i <= 4; i++ {
		state.Chapters = append(state.Chapters, Chapter{Number: i, Title: "c", Status: StatusPending})
	}
	executor := executorFor(worker, dirtyRunner())
	executor.Budget = SagaBudget{CostLimit: 1.00, MaxChapters: 10}

	reason, _ := executor.Run(context.Background(), "/repo", state)

	if reason != StopCostLimit {
		t.Fatalf("reason = %q, want cost-limit", reason)
	}
	// Two chapters at $0.60 crosses $1.00; a third would be spending past a
	// ceiling the user set.
	if len(worker.worked) != 2 {
		t.Fatalf("worked %d chapters, want 2", len(worker.worked))
	}
}

func TestRepeatedFailuresStopTheRun(t *testing.T) {
	worker := &workerSpy{err: errors.New("still broken")}
	state := &SagaState{Goal: "g"}
	for i := 1; i <= 6; i++ {
		state.Chapters = append(state.Chapters, Chapter{Number: i, Title: "c", Status: StatusPending})
	}
	executor := executorFor(worker, dirtyRunner())
	executor.Budget = SagaBudget{DoomThreshold: 2, MaxChapters: 10}

	reason, _ := executor.Run(context.Background(), "/repo", state)

	// The point of the doom-loop guard is that a saga failing the same way
	// forever costs money forever.
	if reason != StopDoomLoop {
		t.Fatalf("reason = %q, want doom-loop", reason)
	}
	if len(worker.worked) > 3 {
		t.Fatalf("kept going for %d chapters", len(worker.worked))
	}
}

func TestARunWithNothingLeftToDoSaysSo(t *testing.T) {
	state := oneChapter(StatusDone)
	worker := &workerSpy{}

	reason, err := executorFor(worker, dirtyRunner()).Run(context.Background(), "/repo", state)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if reason != StopNoWork {
		t.Fatalf("reason = %q, want no-work", reason)
	}
	if len(worker.worked) != 0 {
		t.Fatalf("worked a completed chapter: %v", worker.worked)
	}
}

func TestCancellingTheRunStopsIt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	worker := &workerSpy{}
	state := &SagaState{Goal: "g", Chapters: []Chapter{{Number: 1, Title: "c", Status: StatusPending}}}

	reason, err := executorFor(worker, dirtyRunner()).Run(ctx, "/repo", state)

	// `kolk saga stop` and Ctrl+C both arrive as a cancelled context.
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if len(worker.worked) != 0 {
		t.Fatalf("worked %v after cancellation", worker.worked)
	}
	if reason != StopNone {
		t.Fatalf("reason = %q; cancellation is not a budget stop", reason)
	}
}
