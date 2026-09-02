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

type cancellingWorker struct {
	cancel context.CancelFunc
}

type persistedWorker struct {
	persisted *bool
	worked    bool
}

func (w *cancellingWorker) Work(_ context.Context, _ Chapter, _ string) (WorkResult, error) {
	w.cancel()
	return WorkResult{}, context.Canceled
}

func (w *persistedWorker) Work(_ context.Context, _ Chapter, _ string) (WorkResult, error) {
	if !*w.persisted {
		return WorkResult{}, errors.New("worker started before executing state was persisted")
	}
	w.worked = true
	return WorkResult{}, nil
}

type cancellingStatusRunner struct {
	cancel context.CancelFunc
	asked  []string
}

func (r *cancellingStatusRunner) Run(_ context.Context, command, _ string) (CommandResult, error) {
	r.asked = append(r.asked, command)
	if command == "git status --porcelain" {
		r.cancel()
		return CommandResult{Output: " M main.go\n"}, nil
	}
	return CommandResult{}, nil
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

func TestAChapterPersistsExecutingBeforeWorkerStarts(t *testing.T) {
	persisted := false
	worker := &persistedWorker{persisted: &persisted}
	executor := executorFor(worker, dirtyRunner())
	executor.Write = func(_ string, data []byte, _ os.FileMode) error {
		var state SagaState
		parsed, err := ParseSagaMarkdown(string(data))
		if err != nil {
			t.Fatalf("ParseSagaMarkdown: %v", err)
		}
		state = *parsed
		if state.Chapters[0].Status == StatusExecuting {
			persisted = true
		}
		return nil
	}

	if _, err := executor.RunWake(context.Background(), "/repo", oneChapter(StatusPending)); err != nil {
		t.Fatalf("RunWake: %v", err)
	}
	if !worker.worked {
		t.Fatal("worker did not run after the executing marker was persisted")
	}
}

func TestAPlannedChapterPersistsExecutingBeforeWorkerStarts(t *testing.T) {
	persisted := false
	worker := &persistedWorker{persisted: &persisted}
	executor := plannedRunner(&plannerSpy{titles: []string{"first"}}, worker)
	writes := 0
	executor.Write = func(_ string, data []byte, _ os.FileMode) error {
		writes++
		state, err := ParseSagaMarkdown(string(data))
		if err != nil {
			t.Fatalf("ParseSagaMarkdown: %v", err)
		}
		if writes == 1 && (len(state.Chapters) != 1 || state.Chapters[0].Status != StatusExecuting) {
			t.Fatalf("first planned artifact = %q, want one executing chapter", data)
		}
		if writes == 1 {
			persisted = true
		}
		return nil
	}

	if _, err := executor.RunWake(context.Background(), "/repo", &SagaState{Goal: "make it work"}); err != nil {
		t.Fatalf("RunWake: %v", err)
	}
	if !worker.worked || writes < 2 {
		t.Fatal("planned worker did not run after the executing marker was persisted")
	}
}

func TestAWakeFailsWhenTheArtifactCannotBeWritten(t *testing.T) {
	worker := &workerSpy{summary: "completed change"}
	state := &SagaState{
		Goal:     "make it work",
		Chapters: []Chapter{{Number: 1, Title: "first", Status: StatusPending}},
	}
	executor := executorFor(worker, dirtyRunner())
	executor.Budget = SagaBudget{MaxChapters: 10, CostLimit: 100}
	executor.Write = func(string, []byte, os.FileMode) error { return os.ErrPermission }

	if _, err := executor.RunWake(context.Background(), "/repo", state); err == nil {
		t.Fatal("RunWake reported success after artifact persistence failed")
	} else if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("RunWake error = %v, want the writer error", err)
	}
	if state.Chapters[0].Status != StatusExecuting {
		t.Fatalf("chapter status = %q, want durable executing state before work", state.Chapters[0].Status)
	}
	if len(worker.worked) != 0 {
		t.Fatalf("worker ran despite artifact persistence failure: %v", worker.worked)
	}
}

func TestAWakePersistsGoalCompletionAsATerminalState(t *testing.T) {
	state := &SagaState{
		Goal:     "make it work",
		Criteria: []AcceptanceCriterion{{Description: "it works", Done: true}},
	}
	writes := 0
	executor := executorFor(&workerSpy{}, dirtyRunner())
	executor.Write = func(_ string, data []byte, _ os.FileMode) error {
		writes++
		if !strings.Contains(string(data), "- **Status**: completed") {
			t.Fatalf("persisted terminal artifact = %q, want completed status", data)
		}
		return nil
	}

	reason, err := executor.RunWake(context.Background(), "/repo", state)
	if err != nil {
		t.Fatalf("RunWake: %v", err)
	}
	if reason != StopGoalComplete || state.Status != SagaStatusCompleted || writes != 1 {
		t.Fatalf("reason=%q status=%q writes=%d; want completed state persisted once", reason, state.Status, writes)
	}
}

func TestAWakeReportsTerminalStatePersistenceFailure(t *testing.T) {
	state := &SagaState{
		Goal:     "make it work",
		Criteria: []AcceptanceCriterion{{Description: "it works", Done: true}},
	}
	executor := executorFor(&workerSpy{}, dirtyRunner())
	executor.Write = func(string, []byte, os.FileMode) error { return os.ErrPermission }

	reason, err := executor.RunWake(context.Background(), "/repo", state)
	if reason != StopNone {
		t.Fatalf("reason = %q, want no successful stop when terminal state is not durable", reason)
	}
	if err == nil || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("RunWake error = %v, want the terminal artifact writer error", err)
	}
	if state.Status != SagaStatusCompleted {
		t.Fatalf("in-memory status = %q, want completed for truthful retry diagnostics", state.Status)
	}
}

func TestAWakeDoesNotReopenACompletedSaga(t *testing.T) {
	state := &SagaState{
		Goal:     "already done",
		Status:   SagaStatusCompleted,
		Chapters: []Chapter{{Number: 1, Title: "do not rerun", Status: StatusPending}},
	}
	worker := &workerSpy{}

	reason, err := executorFor(worker, dirtyRunner()).RunWake(context.Background(), "/repo", state)
	if err != nil {
		t.Fatalf("RunWake: %v", err)
	}
	if reason != StopGoalComplete || len(worker.worked) != 0 {
		t.Fatalf("reason=%q worked=%v; want terminal completion without reopening work", reason, worker.worked)
	}
}

func TestAWakeDoesNotReopenABlockedSaga(t *testing.T) {
	state := &SagaState{
		Goal:     "blocked",
		Status:   SagaStatusBlocked,
		Chapters: []Chapter{{Number: 1, Title: "do not rerun", Status: StatusPending}},
	}
	worker := &workerSpy{}

	reason, err := executorFor(worker, dirtyRunner()).RunWake(context.Background(), "/repo", state)
	if err != nil {
		t.Fatalf("RunWake: %v", err)
	}
	if reason != StopDoomLoop || len(worker.worked) != 0 {
		t.Fatalf("reason=%q worked=%v; want blocked terminal state without reopening work", reason, worker.worked)
	}
}

func TestAWakePersistsExecutingWithoutAStrikeWhenWorkerIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	worker := &cancellingWorker{cancel: cancel}
	state := &SagaState{
		Goal:     "make it work",
		Chapters: []Chapter{{Number: 1, Title: "first", Status: StatusPending}},
	}
	executor := executorFor(worker, dirtyRunner())
	executor.Budget = SagaBudget{MaxChapters: 10, CostLimit: 100}
	writes := 0
	executor.Write = func(_ string, data []byte, _ os.FileMode) error {
		writes++
		if !strings.Contains(string(data), "- **Status**: in-progress (Chapter 1") {
			t.Fatalf("persisted artifact = %q, want resumable executing state", data)
		}
		return nil
	}

	reason, err := executor.RunWake(ctx, "/repo", state)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunWake error = %v, want context.Canceled", err)
	}
	if reason != StopNone || state.Chapters[0].Status != StatusExecuting || state.Strikes != 0 || writes == 0 {
		t.Fatalf("reason=%q chapter=%q strikes=%d writes=%d; want cancelled executing state without strike", reason, state.Chapters[0].Status, state.Strikes, writes)
	}
}

func TestAWakePreservesCancellationArtifactFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	worker := &cancellingWorker{cancel: cancel}
	state := &SagaState{
		Goal:     "make it work",
		Chapters: []Chapter{{Number: 1, Title: "first", Status: StatusPending}},
	}
	executor := executorFor(worker, dirtyRunner())
	writes := 0
	executor.Write = func(string, []byte, os.FileMode) error {
		writes++
		if writes == 1 {
			return nil
		}
		return os.ErrPermission
	}

	reason, err := executor.RunWake(ctx, "/repo", state)
	if reason != StopNone {
		t.Fatalf("reason = %q, want no successful stop after cancellation persistence failure", reason)
	}
	if !errors.Is(err, context.Canceled) || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("RunWake error = %v, want both cancellation and artifact errors", err)
	}
	if state.Chapters[0].Status != StatusExecuting || state.Strikes != 0 || writes != 2 {
		t.Fatalf("chapter=%q strikes=%d writes=%d; want resumable executing state and two persistence attempts", state.Chapters[0].Status, state.Strikes, writes)
	}
}

func TestSagaCancellationResultPreservesJoinedCauses(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	original := errors.Join(context.Canceled, os.ErrPermission)

	got := sagaCancellationResult(ctx, original)
	if !errors.Is(got, context.Canceled) || !errors.Is(got, os.ErrPermission) {
		t.Fatalf("cancellation result = %v, want both joined causes", got)
	}
}

func TestRunPreservesCancellationArtifactFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	worker := &cancellingWorker{cancel: cancel}
	state := &SagaState{
		Goal:     "make it work",
		Chapters: []Chapter{{Number: 1, Title: "first", Status: StatusPending}},
	}
	executor := executorFor(worker, dirtyRunner())
	writes := 0
	executor.Write = func(string, []byte, os.FileMode) error {
		writes++
		if writes == 1 {
			return nil
		}
		return os.ErrPermission
	}

	reason, err := executor.Run(ctx, "/repo", state)
	if reason != StopNone {
		t.Fatalf("reason = %q, want no successful stop after cancellation persistence failure", reason)
	}
	if !errors.Is(err, context.Canceled) || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("Run error = %v, want both cancellation and artifact errors", err)
	}
	if state.Chapters[0].Status != StatusExecuting || state.Strikes != 0 || writes != 2 {
		t.Fatalf("chapter=%q strikes=%d writes=%d; want resumable executing state and two persistence attempts", state.Chapters[0].Status, state.Strikes, writes)
	}
}

func TestAWakePersistsExecutingWithoutAStrikeWhenVerificationIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runner := &cancellingStatusRunner{cancel: cancel}
	state := &SagaState{
		Goal:     "make it work",
		Chapters: []Chapter{{Number: 1, Title: "first", Status: StatusPending}},
	}
	executor := executorFor(&workerSpy{}, runner)
	executor.Detector = fixedDetector{{Name: "test", Command: "go test ./..."}}
	executor.Budget = SagaBudget{MaxChapters: 10, CostLimit: 100}
	writes := 0
	executor.Write = func(_ string, data []byte, _ os.FileMode) error {
		writes++
		if !strings.Contains(string(data), "- **Status**: in-progress (Chapter 1") {
			t.Fatalf("persisted artifact = %q, want resumable executing state", data)
		}
		return nil
	}

	reason, err := executor.RunWake(ctx, "/repo", state)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunWake error = %v, want context.Canceled", err)
	}
	if reason != StopNone || state.Chapters[0].Status != StatusExecuting || state.Strikes != 0 || writes == 0 {
		t.Fatalf("reason=%q chapter=%q strikes=%d writes=%d; want cancelled executing state without strike", reason, state.Chapters[0].Status, state.Strikes, writes)
	}
	if len(runner.asked) != 1 || runner.asked[0] != "git status --porcelain" {
		t.Fatalf("verification commands = %v, want only the status check before cancellation", runner.asked)
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

func TestAWakeWorksExactlyOneChapter(t *testing.T) {
	worker := &workerSpy{summary: "first change"}
	state := &SagaState{
		Goal:        "make it work",
		MaxChapters: 10,
		CostLimit:   100,
		Chapters: []Chapter{
			{Number: 1, Title: "first", Status: StatusPending},
			{Number: 2, Title: "second", Status: StatusPending},
		},
	}
	executor := executorFor(worker, dirtyRunner())
	executor.Budget = SagaBudget{MaxChapters: 10, CostLimit: 100}

	reason, err := executor.RunWake(context.Background(), "/repo", state)
	if err != nil {
		t.Fatalf("RunWake: %v", err)
	}
	if reason != StopWake {
		t.Fatalf("reason = %q, want wake-complete", reason)
	}
	if len(worker.worked) != 1 || worker.worked[0] != "first" {
		t.Fatalf("worked = %v, want exactly the first chapter", worker.worked)
	}
	if state.Chapters[0].Status != StatusDone || state.Chapters[1].Status != StatusPending {
		t.Fatalf("chapters = %+v, want first done and second pending", state.Chapters)
	}
}

func TestAWakeRecordsTheSelectedActiveChapter(t *testing.T) {
	worker := &workerSpy{summary: "later change"}
	state := &SagaState{
		Goal:          "make it work",
		ActiveChapter: 1,
		MaxChapters:   10,
		CostLimit:     100,
		Chapters: []Chapter{
			{Number: 4, Title: "later", Status: StatusPending},
		},
	}
	executor := executorFor(worker, dirtyRunner())
	executor.Budget = SagaBudget{MaxChapters: 10, CostLimit: 100}

	if _, err := executor.RunWake(context.Background(), "/repo", state); err != nil {
		t.Fatalf("RunWake: %v", err)
	}
	if state.ActiveChapter != 4 {
		t.Fatalf("active chapter = %d, want selected chapter 4", state.ActiveChapter)
	}
}

func TestAWakePlansAndWorksOnlyOneChapter(t *testing.T) {
	planner := &plannerSpy{titles: []string{"first", "second"}}
	worker := &workerSpy{}
	executor := plannedRunner(planner, worker)
	state := &SagaState{Goal: "g"}

	reason, err := executor.RunWake(context.Background(), "/repo", state)
	if err != nil {
		t.Fatalf("RunWake: %v", err)
	}
	if reason != StopWake || planner.calls != 1 || len(worker.worked) != 1 || len(state.Chapters) != 1 {
		t.Fatalf("reason=%q planner-calls=%d worked=%v chapters=%d; want one bounded wake", reason, planner.calls, worker.worked, len(state.Chapters))
	}
}

func TestAWakePersistsAndStopsAfterOneFailedChapter(t *testing.T) {
	worker := &workerSpy{err: errors.New("worker failed")}
	state := &SagaState{
		Goal:        "g",
		MaxChapters: 10,
		CostLimit:   100,
		Chapters: []Chapter{
			{Number: 1, Title: "first", Status: StatusPending},
			{Number: 2, Title: "second", Status: StatusPending},
		},
	}
	executor := executorFor(worker, dirtyRunner())
	executor.Budget = SagaBudget{MaxChapters: 10, CostLimit: 100}
	writes := 0
	executor.Write = func(_ string, data []byte, _ os.FileMode) error {
		writes++
		body := string(data)
		switch writes {
		case 1:
			if !strings.Contains(body, "- **Status**: executing") {
				t.Fatalf("first persisted artifact = %q, want executing marker", data)
			}
		case 2:
			if !strings.Contains(body, "- **Status**: failed") {
				t.Fatalf("final persisted artifact = %q, want the failed chapter", data)
			}
		}
		return nil
	}

	if _, err := executor.RunWake(context.Background(), "/repo", state); err == nil {
		t.Fatal("failed wake reported success")
	}
	if len(worker.worked) != 1 || writes != 2 || state.Chapters[0].Status != StatusFailed || state.Chapters[1].Status != StatusPending {
		t.Fatalf("worked=%v writes=%d chapters=%+v; want one persisted failure and no second chapter", worker.worked, writes, state.Chapters)
	}
}

func TestAWakeStopsAtAReachedCostLimitAfterOneChapter(t *testing.T) {
	worker := &workerSpy{cost: 2}
	state := &SagaState{
		Goal:     "g",
		Chapters: []Chapter{{Number: 1, Title: "first", Status: StatusPending}, {Number: 2, Title: "second", Status: StatusPending}},
	}
	executor := executorFor(worker, dirtyRunner())
	executor.Budget = SagaBudget{MaxChapters: 10, CostLimit: 1}

	reason, err := executor.RunWake(context.Background(), "/repo", state)
	if err != nil {
		t.Fatalf("RunWake: %v", err)
	}
	if reason != StopCostLimit || len(worker.worked) != 1 || state.Chapters[1].Status != StatusPending {
		t.Fatalf("reason=%q worked=%v chapters=%+v; want cost stop after one chapter", reason, worker.worked, state.Chapters)
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

// cancellingCommitter is a checkpointer whose commit fails for a real reason
// at the same moment the user cancels — a hook exiting non-zero while Ctrl+C
// lands.
type cancellingCommitter struct{ cancel context.CancelFunc }

func (c *cancellingCommitter) HasChanges(string) (bool, error) { return true, nil }
func (c *cancellingCommitter) RollbackChapter(string) error    { return nil }
func (c *cancellingCommitter) CommitChapter(string, int, string) (string, error) {
	c.cancel()
	return "", errors.New("pre-commit hook exited 1")
}

// A commit that failed while the user was cancelling is the one error the
// next wake most needs to see. The verifier used the plain cancellation form
// and returned bare context.Canceled, so the operator saw "(interrupted)" and
// nothing about the hook.
func TestACancelledCommitKeepsTheGitError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	verifier := &ChapterVerifier{Detector: noGates{}, Checkpointer: &cancellingCommitter{cancel: cancel}}

	_, err := verifier.Verify(ctx, "repo", Chapter{Number: 1, Title: "first"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Verify error = %v, want cancellation kept as the terminal outcome", err)
	}
	if !strings.Contains(err.Error(), "pre-commit hook exited 1") {
		t.Fatalf("Verify error = %v, want the commit failure preserved", err)
	}

	// And through the lifecycle, where the chapter must be left resumable.
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	state := oneChapter(StatusExecuting)
	verifier = &ChapterVerifier{Detector: noGates{}, Checkpointer: &cancellingCommitter{cancel: cancel}}
	err = VerifyChapter(ctx, verifier, "repo", state, 0)
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "pre-commit hook exited 1") {
		t.Fatalf("VerifyChapter error = %v, want cancellation joined with the commit failure", err)
	}
	if state.Chapters[0].Status != StatusExecuting {
		t.Fatalf("chapter status after cancelled commit = %q, want executing (resumable, no strike)", state.Chapters[0].Status)
	}
	if state.Strikes != 0 {
		t.Fatalf("strikes = %d, want none for a cancellation", state.Strikes)
	}
}
