package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

type artifactPersistError struct {
	err error
}

func (e *artifactPersistError) Error() string {
	return fmt.Sprintf("saga: could not write SAGA.md: %v", e.err)
}

func (e *artifactPersistError) Unwrap() error { return e.err }

// plannerError marks a failure to choose the next chapter, as opposed to a
// failure to work one.
//
// The distinction is the whole reason it exists. A chapter that fails is
// ordinary — a continuous run counts it and tries the next one. A planner that
// fails has produced no chapter to try, so counting it as a failure and looping
// is asking a broken planner the same question forever. The message is the
// inner one, so nothing a user reads changes.
type plannerError struct{ err error }

func (e *plannerError) Error() string { return e.err.Error() }
func (e *plannerError) Unwrap() error { return e.err }

// StopNoWork halts a run that has no chapter left to attempt.
//
// Distinct from StopGoalComplete: the chapters are done, which is not the same
// as the goal being met. Only the acceptance criteria can say that, and a saga
// that ran out of plan should say so rather than claim success.
const StopNoWork StopReason = "no-work"

// WorkResult is what doing a chapter's work cost and produced.
type WorkResult struct {
	CostUSD float64
	Summary string
}

// ChapterPlanner chooses the next chapter, one at a time.
//
// One at a time, and only after seeing what the last one achieved, because
// docs/plan/10-saga-loop.md §1.1 asks for "exactly one discrete, manageable
// task that moves closer to the goal" chosen from the current state. A plan
// written up front cannot know what chapter three learned, and a saga whose
// plan is already wrong by chapter three is one nobody can trust to keep going.
//
// An empty title means the goal is met and there is nothing left to plan.
type ChapterPlanner interface {
	Next(ctx context.Context, goal string, done []Chapter) (string, error)
}

// ChapterWorker performs one chapter's actual work.
//
// A port rather than the Agent itself, so the loop that spends a budget and
// counts failures can be tested without a provider, and so the thing doing the
// work can later be a subagent, a different model, or a person.
type ChapterWorker interface {
	Work(ctx context.Context, chapter Chapter, goal string) (WorkResult, error)
}

// SagaRunner drives chapters: work, verify, record, repeat until a budget says
// stop.
//
// This is the half of the saga that was specified and never built. The state
// machine, the gates, the budget guards and the artifact writer all existed and
// nothing called them, because nothing walked the chapters.
type SagaRunner struct {
	// Planner decides the next chapter. Nil means the chapters are already
	// written — by a person, in SAGA.md — and the run works those and stops.
	Planner ChapterPlanner
	Worker  ChapterWorker
	// Repairer gets one turn to fix a chapter its gates rejected. Nil rolls
	// the chapter back instead, which is what happened before S10.11.
	Repairer ChapterRepairer
	Runner   CommandRunner
	Detector QualityGateDetector
	Budget   SagaBudget
	Write    ArtifactWriter
	// Now is the clock, replaceable so a test can reach a timeout without
	// waiting for one.
	Now func() time.Time
	// Out receives progress. A saga that runs for an hour in silence is one
	// nobody can tell from a hang.
	Out io.Writer
	// Spent reads what the session has spent so far. The worker reports its
	// own cost; the planner and the repair turn run on the same agent and
	// report nothing, so their spend is measured as the meter's change around
	// them and charged to the saga (V34.3d). Nil means those calls are free.
	Spent func() float64
}

// step is one chapter attempt, and the only path to one.
//
// Everything before the work is here — the cancellation check, the artifact's
// terminal status, the budget, and choosing or planning the chapter — because
// Run and RunWake had a copy each and the copies had already drifted: one
// counted failures in a local variable and the other in the artifact, one
// checked the budget after its chapter and the other before the next. Keeping
// the pre-work in one place is what stops a third caller reopening a completed
// or blocked saga, which is the failure this guards.
//
// What is deliberately NOT here is what to do about a chapter that failed. A
// wake stops and reports it; a continuous run counts it and tries the next
// chapter. That is a policy difference between two callers, not duplication,
// so each states it in its own words.
//
// A non-empty reason means the saga stopped, and the stop is already
// persisted. A nil error with an empty reason means one chapter was worked and
// verified.
func (r *SagaRunner) step(ctx context.Context, repoDir string, state *SagaState, failures int, elapsed time.Duration) (StopReason, error) {
	if err := ctx.Err(); err != nil {
		// Cancellation is the user stopping, not the budget refusing.
		return StopNone, err
	}
	if reason := terminalSagaStop(state); reason != StopNone {
		return r.finishStop(repoDir, state, reason)
	}
	if reason := r.Budget.Check(state, failures, elapsed); reason != StopNone {
		return r.finishStop(repoDir, state, reason)
	}

	index, ok := nextChapter(state)
	if !ok {
		planned, err := r.planNext(ctx, state)
		if err != nil {
			return StopNone, &plannerError{err: err}
		}
		if !planned {
			return r.finishStop(repoDir, state, r.noMoreWork())
		}
		index = len(state.Chapters) - 1
	}

	if err := r.RunChapter(ctx, repoDir, state, index); err != nil {
		if cancelErr := sagaCancellationResult(ctx, err); cancelErr != nil {
			// Preserve cleanup failures alongside cancellation. Returning only
			// ctx.Err() would make a failed durable resume boundary invisible.
			return StopNone, cancelErr
		}
		var persistErr *artifactPersistError
		if errors.As(err, &persistErr) {
			// The chapter has not been worked when its pre-work marker could
			// not be persisted. Do not describe that as a chapter failure or
			// spend a strike on storage being unavailable.
			return StopNone, err
		}
		r.say("chapter %d failed: %v", state.Chapters[index].Number, err)
		return StopNone, err
	}
	if cancelErr := sagaCancellationResult(ctx, nil); cancelErr != nil {
		return StopNone, cancelErr
	}
	return StopNone, nil
}

// RunWake advances at most one chapter and then returns. Planning one chapter
// and working it count as one wake; a planner is never called a second time in
// the same invocation. The older Run method remains the multi-chapter API for
// callers that explicitly want a continuous loop.
func (r *SagaRunner) RunWake(ctx context.Context, repoDir string, state *SagaState) (StopReason, error) {
	if state == nil {
		return StopNone, fmt.Errorf("saga: state is required")
	}
	started := r.now()
	// The wake counts failures where they are durable: the artifact's own
	// strike line, which outlives the process, rather than a variable that
	// starts at zero every time a wake begins.
	reason, err := r.step(ctx, repoDir, state, state.Strikes, 0)
	if reason != StopNone {
		return reason, err
	}
	if err != nil {
		if state.Status == SagaStatusBlocked {
			return StopDoomLoop, nil
		}
		return StopNone, err
	}
	if reason := r.Budget.Check(state, state.Strikes, r.now().Sub(started)); reason != StopNone {
		return r.finishStop(repoDir, state, reason)
	}
	return StopWake, nil
}

// RunChapter takes one chapter from pending to verified.
func (r *SagaRunner) RunChapter(ctx context.Context, repoDir string, state *SagaState, index int) error {
	if state == nil || index < 0 || index >= len(state.Chapters) {
		return fmt.Errorf("saga: chapter index %d out of range", index)
	}
	chapter := &state.Chapters[index]
	// Keep the durable progress marker aligned with the chapter being worked.
	// A wake may resume at chapter N after earlier chapters were persisted; a
	// stale marker would make both SAGA.md and the CLI report the wrong chapter.
	state.ActiveChapter = chapter.Number

	if err := r.advanceToExecuting(chapter); err != nil {
		return err
	}
	// Mark the tree before the worker touches it, so a failed chapter can be
	// rolled back to exactly here -- the user's uncommitted work included --
	// and so the mark is in the persisted state a restart would read (V34.3c).
	// A mark that cannot be taken is said, and the rollback stays conservative.
	if mark, err := NewCommandCheckpointer(ctx, r.Runner).MarkChapter(repoDir); err == nil {
		chapter.Mark = &mark
	} else {
		r.say("chapter %d: no rollback mark (%v); a rollback would restore tracked files only", chapter.Number, err)
	}
	// Persist the in-flight marker before the worker can mutate the repository.
	// A crash or an unavailable artifact writer must leave a truthful resume
	// boundary; starting work first would make the durable state claim that the
	// chapter is still pending and invite a duplicate attempt.
	if err := r.persist(repoDir, state); err != nil {
		return err
	}
	r.say("chapter %d: %s", chapter.Number, chapter.Title)

	begun := r.now()
	result, workErr := r.Worker.Work(ctx, *chapter, state.Goal)

	// Cost is recorded whether or not the work succeeded. A failed attempt
	// still spent money, and a budget that only counts successes is a budget
	// that cannot stop a saga failing expensively.
	chapter.CostUSD += result.CostUSD
	state.CumulativeCost += result.CostUSD
	chapter.DurationSec += int(r.now().Sub(begun).Seconds())
	if cancelErr := sagaCancellation(ctx, workErr); cancelErr != nil {
		if persistErr := r.persist(repoDir, state); persistErr != nil {
			return errors.Join(cancelErr, persistErr)
		}
		return cancelErr
	}

	if workErr != nil {
		// No verification: gates on work that was never done would commit
		// whatever happened to be in the tree and call it the chapter.
		if err := transitionChapter(chapter, StatusFailed); err != nil {
			return err
		}
		chapter.Verification = workErr.Error()
		if strikeErr := RecordGateFailure(state); strikeErr != nil {
			return strikeErr
		}
		workFailure := fmt.Errorf("saga: chapter %d could not be worked: %w", chapter.Number, workErr)
		if persistErr := r.persist(repoDir, state); persistErr != nil {
			return errors.Join(workFailure, persistErr)
		}
		return workFailure
	}

	if result.Summary != "" {
		chapter.Changes = append(chapter.Changes, result.Summary)
	}

	// Verification runs the gates and, when they fail, the one repair turn; the
	// repair spends on the same agent and reports nothing, so it is charged
	// here as the meter's change (V34.3d).
	beforeVerify := r.spent()
	verifyErr := VerifyChapter(ctx, r.verifier(ctx), repoDir, state, index)
	if repaired := r.spent() - beforeVerify; repaired > 0 {
		chapter.CostUSD += repaired
		state.CumulativeCost += repaired
	}
	if persistErr := r.persist(repoDir, state); persistErr != nil {
		if verifyErr != nil {
			return errors.Join(verifyErr, persistErr)
		}
		return persistErr
	}
	return verifyErr
}

// verifier assembles the ports for one chapter's verification.
//
// Built here rather than inside VerifyChapter so the state machine takes a
// verifier and not the four things one is made of — the parameter list was
// already long and the repair turn would have made it longer.
func (r *SagaRunner) verifier(ctx context.Context) *ChapterVerifier {
	detector := r.Detector
	if detector == nil {
		// A nil port would panic on the first Detect. Defaulting says the
		// useful thing instead: no detector given means "work it out from the
		// repository".
		detector = FileGateDetector{}
	}
	return &ChapterVerifier{
		Detector:     detector,
		Runner:       NewCommandGateRunner(ctx, r.Runner),
		Checkpointer: NewCommandCheckpointer(ctx, r.Runner),
		Repairer:     r.Repairer,
	}
}

// advanceToExecuting walks a chapter to the state work begins in.
func (r *SagaRunner) advanceToExecuting(chapter *Chapter) error {
	switch chapter.Status {
	case StatusExecuting:
		return nil
	case StatusFailed:
		// A retry re-plans first; the transition table says so.
		if err := transitionChapter(chapter, StatusPlanning); err != nil {
			return err
		}
	case StatusPending:
		if err := transitionChapter(chapter, StatusPlanning); err != nil {
			return err
		}
	case StatusPlanning:
	default:
		return fmt.Errorf("saga: chapter %d is %q and cannot be worked", chapter.Number, chapter.Status)
	}
	return transitionChapter(chapter, StatusExecuting)
}

// planNext asks the planner for one more chapter, if there is a planner.
func (r *SagaRunner) planNext(ctx context.Context, state *SagaState) (bool, error) {
	if r.Planner == nil {
		return false, nil
	}
	before := r.spent()
	title, err := r.Planner.Next(ctx, state.Goal, state.Chapters)
	// Planning cost is saga cost whether or not a chapter came of it; a budget
	// that cannot see it is one a saga can plan its way past (V34.3d).
	planned := r.spent() - before
	state.CumulativeCost += planned
	if err != nil {
		// Returning an error rather than stopping quietly: a planner that
		// cannot answer is a broken saga, not a finished one.
		return false, fmt.Errorf("saga: planning the next chapter: %w", err)
	}
	if title == "" {
		return false, nil
	}
	state.Chapters = append(state.Chapters, Chapter{
		Number:  len(state.Chapters) + 1,
		Title:   title,
		Status:  StatusPending,
		CostUSD: planned,
	})
	return true, nil
}

// noMoreWork distinguishes a finished goal from an exhausted plan.
//
// With a planner, nothing left to plan means the planner judged the goal met.
// Without one, it only means the hand-written chapters ran out, which says
// nothing about the goal.
func (r *SagaRunner) noMoreWork() StopReason {
	if r.Planner != nil {
		return StopGoalComplete
	}
	return StopNoWork
}

// terminalSagaStop treats durable terminal state as authoritative. SAGA.md is
// the restart boundary; reopening a completed or blocked artifact because a
// pending chapter happens to remain would silently violate that boundary.
func terminalSagaStop(state *SagaState) StopReason {
	switch state.Status {
	case SagaStatusCompleted:
		return StopGoalComplete
	case SagaStatusBlocked:
		return StopDoomLoop
	default:
		return StopNone
	}
}

// finishStop records only terminal whole-saga outcomes. Chapter and budget
// state is already persisted at its mutation boundary; a budget stop remains
// resumable and must not be mislabeled as terminal.
func (r *SagaRunner) finishStop(repoDir string, state *SagaState, reason StopReason) (StopReason, error) {
	var status string
	switch reason {
	case StopGoalComplete:
		status = SagaStatusCompleted
	case StopDoomLoop:
		status = SagaStatusBlocked
	default:
		return reason, nil
	}
	if state.Status == status {
		return reason, nil
	}
	state.Status = status
	if err := r.persist(repoDir, state); err != nil {
		return StopNone, err
	}
	return reason, nil
}

// nextChapter finds the first chapter still worth attempting.
func nextChapter(state *SagaState) (int, bool) {
	for i := range state.Chapters {
		switch state.Chapters[i].Status {
		case StatusPending, StatusPlanning, StatusExecuting, StatusFailed:
			return i, true
		}
	}
	return 0, false
}

// persist writes SAGA.md, reporting a failure without losing the chapter.
//
// The artifact is a convenience for resuming; a chapter that ran and verified
// has still happened, and turning a write failure into a chapter failure would
// discard real work over a full disk.
func (r *SagaRunner) persist(repoDir string, state *SagaState) error {
	if r.Write == nil {
		return nil
	}
	if err := SaveSagaArtifact(repoDir, state, r.Write); err != nil {
		r.say("warning: could not write SAGA.md: %v", err)
		return &artifactPersistError{err: err}
	}
	return nil
}

func (r *SagaRunner) say(format string, args ...any) {
	if r.Out == nil {
		return
	}
	fmt.Fprintf(r.Out, format+"\n", args...)
}

func (r *SagaRunner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func sagaCancellation(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

// sagaCancellationResult keeps cancellation as the terminal outcome while
// preserving any error returned while recording the resumable state. A plain
// ctx.Err() would hide a failed cleanup write and leave the operator without a
// truthful explanation of what can be resumed.
func sagaCancellationResult(ctx context.Context, err error) error {
	cancelErr := sagaCancellation(ctx, err)
	if cancelErr == nil {
		return nil
	}
	if err == nil {
		return cancelErr
	}
	if ctxErr := ctx.Err(); ctxErr == nil || errors.Is(err, ctxErr) {
		return err
	}
	return errors.Join(cancelErr, err)
}

// spent reads the session meter, or zero when none is wired.
func (r *SagaRunner) spent() float64 {
	if r.Spent == nil {
		return 0
	}
	return r.Spent()
}
