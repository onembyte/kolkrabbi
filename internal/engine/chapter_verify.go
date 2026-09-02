package engine

import (
	"context"
	"fmt"
	"strings"
)

// ChapterVerifier orchestrates the verify-then-checkpoint cycle for a single
// saga chapter. It depends only on engine ports (QualityGateRunner,
// GitCheckpointer, QualityGateDetector), never on shell or platform packages.
type ChapterVerifier struct {
	Detector     QualityGateDetector
	Runner       QualityGateRunner
	Checkpointer GitCheckpointer
	// Repairer gets one turn to fix a regression before the chapter's work is
	// discarded. Nil skips straight to rollback.
	Repairer ChapterRepairer
}

// ChapterRepairer gets one attempt to fix what the quality gates rejected.
//
// One attempt, because docs/plan/10-saga-loop.md §1.1 step 3 says so and
// because the alternative is a loop: a chapter that cannot fix itself twice
// will not fix itself at all, and each attempt costs a model turn. The gate
// output is passed along, since "fix the regression" is not an instruction
// without the regression.
type ChapterRepairer interface {
	Repair(ctx context.Context, chapter Chapter, gateOutput string) error
}

// VerifyResult is the outcome of a single chapter verification attempt.
type VerifyResult struct {
	Passed   bool
	Commit   string // short hash if committed; empty on failure
	Strikes  int    // how many consecutive failures the caller should add
	GateRuns []GateResult
}

// Verify runs the full verify → commit / rollback cycle for a chapter.
// It returns the verification result. The caller is responsible for
// tracking cumulative strikes.
func (cv *ChapterVerifier) Verify(ctx context.Context, repoDir string, chapter Chapter) (*VerifyResult, error) {
	if cancelErr := sagaCancellation(ctx, nil); cancelErr != nil {
		return nil, cancelErr
	}
	// If no changes were made, skip verification and commit.
	hasChanges, err := cv.Checkpointer.HasChanges(repoDir)
	if err != nil {
		if cancelErr := sagaCancellation(ctx, err); cancelErr != nil {
			return nil, cancelErr
		}
		return nil, fmt.Errorf("checking for changes: %w", err)
	}
	if cancelErr := sagaCancellation(ctx, nil); cancelErr != nil {
		return nil, cancelErr
	}
	if !hasChanges {
		return &VerifyResult{
			Passed:  true,
			Strikes: 0,
		}, nil
	}

	// Detect quality gates.
	gates := cv.Detector.Detect(repoDir)

	// If no gates detected, commit unconditionally.
	if len(gates) == 0 {
		if cancelErr := sagaCancellation(ctx, nil); cancelErr != nil {
			return nil, cancelErr
		}
		commit, err := cv.Checkpointer.CommitChapter(repoDir, chapter.Number, chapter.Title)
		if err != nil {
			if cancelErr := sagaCancellation(ctx, err); cancelErr != nil {
				return nil, cancelErr
			}
			return nil, fmt.Errorf("committing chapter %d: %w", chapter.Number, err)
		}
		return &VerifyResult{
			Passed: true,
			Commit: commit,
		}, nil
	}

	// Run gates. A failure buys one repair turn before the work is discarded:
	// rolling back a nearly-right chapter is expensive, because the next
	// attempt starts from nothing.
	results := cv.Runner.RunGates(repoDir, gates)
	if cancelErr := sagaCancellation(ctx, nil); cancelErr != nil {
		return nil, cancelErr
	}
	if !allGatesPassed(results) && cv.Repairer != nil {
		// A repair that itself fails is not a verifier error — it is simply a
		// chapter that stayed broken, and the rollback below is the answer.
		if err := cv.Repairer.Repair(ctx, chapter, gateFailureOutput(results)); err == nil {
			if cancelErr := sagaCancellation(ctx, nil); cancelErr != nil {
				return nil, cancelErr
			}
			results = cv.Runner.RunGates(repoDir, gates)
		} else if cancelErr := sagaCancellation(ctx, err); cancelErr != nil {
			return nil, cancelErr
		}
	}

	if allGatesPassed(results) {
		if cancelErr := sagaCancellation(ctx, nil); cancelErr != nil {
			return nil, cancelErr
		}
		commit, err := cv.Checkpointer.CommitChapter(repoDir, chapter.Number, chapter.Title)
		if err != nil {
			if cancelErr := sagaCancellation(ctx, err); cancelErr != nil {
				return nil, cancelErr
			}
			return nil, fmt.Errorf("committing chapter %d: %w", chapter.Number, err)
		}
		return &VerifyResult{
			Passed:   true,
			Commit:   commit,
			GateRuns: results,
		}, nil
	}

	// Gates failed — rollback.
	if cancelErr := sagaCancellation(ctx, nil); cancelErr != nil {
		return nil, cancelErr
	}
	if err := cv.Checkpointer.RollbackChapter(repoDir); err != nil {
		if cancelErr := sagaCancellation(ctx, err); cancelErr != nil {
			return nil, cancelErr
		}
		return nil, fmt.Errorf("rolling back chapter %d: %w", chapter.Number, err)
	}

	return &VerifyResult{
		Passed:   false,
		Strikes:  1,
		GateRuns: results,
	}, nil
}

func allGatesPassed(results []GateResult) bool {
	for _, result := range results {
		if !result.Passed {
			return false
		}
	}
	return len(results) > 0
}

// gateFailureOutput is what the repair turn is shown: the failing gates and
// what they said, and nothing about the ones that passed.
func gateFailureOutput(results []GateResult) string {
	var b strings.Builder
	for _, result := range results {
		if result.Passed {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s: %s", result.Gate.Name, strings.TrimSpace(result.Output))
	}
	return b.String()
}
