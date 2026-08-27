package engine

import (
	"context"
	"fmt"
	"io"
	"time"
)

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
	Planner  ChapterPlanner
	Worker   ChapterWorker
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
}

// Run works chapters until the budget, the plan or the user stops it.
func (r *SagaRunner) Run(ctx context.Context, repoDir string, state *SagaState) (StopReason, error) {
	if state == nil {
		return StopNone, fmt.Errorf("saga: state is required")
	}
	started := r.now()
	failures := 0

	for {
		if err := ctx.Err(); err != nil {
			// Cancellation is the user stopping, not the budget refusing.
			return StopNone, err
		}
		if reason := r.Budget.Check(state, failures, r.now().Sub(started)); reason != StopNone {
			return reason, nil
		}

		index, ok := nextChapter(state)
		if !ok {
			planned, err := r.planNext(ctx, state)
			if err != nil {
				return StopNone, err
			}
			if !planned {
				return r.noMoreWork(), nil
			}
			index = len(state.Chapters) - 1
		}

		err := r.RunChapter(ctx, repoDir, state, index)
		if ctx.Err() != nil {
			return StopNone, ctx.Err()
		}
		if err != nil {
			failures++
			r.say("chapter %d failed: %v", state.Chapters[index].Number, err)
			continue
		}
		failures = 0
	}
}

// RunChapter takes one chapter from pending to verified.
func (r *SagaRunner) RunChapter(ctx context.Context, repoDir string, state *SagaState, index int) error {
	if state == nil || index < 0 || index >= len(state.Chapters) {
		return fmt.Errorf("saga: chapter index %d out of range", index)
	}
	chapter := &state.Chapters[index]

	if err := r.advanceToExecuting(chapter); err != nil {
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
		r.persist(repoDir, state)
		return fmt.Errorf("saga: chapter %d could not be worked: %w", chapter.Number, workErr)
	}

	if result.Summary != "" {
		chapter.Changes = append(chapter.Changes, result.Summary)
	}

	verifyErr := VerifyChapter(ctx, r.Runner, repoDir, state, index, r.Detector)
	r.persist(repoDir, state)
	return verifyErr
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
	title, err := r.Planner.Next(ctx, state.Goal, state.Chapters)
	if err != nil {
		// Returning an error rather than stopping quietly: a planner that
		// cannot answer is a broken saga, not a finished one.
		return false, fmt.Errorf("saga: planning the next chapter: %w", err)
	}
	if title == "" {
		return false, nil
	}
	state.Chapters = append(state.Chapters, Chapter{
		Number: len(state.Chapters) + 1,
		Title:  title,
		Status: StatusPending,
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
func (r *SagaRunner) persist(repoDir string, state *SagaState) {
	if r.Write == nil {
		return
	}
	if err := SaveSagaArtifact(repoDir, state, r.Write); err != nil {
		r.say("warning: could not write SAGA.md: %v", err)
	}
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
