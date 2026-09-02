package engine

import (
	"context"
	"fmt"
	"strings"
)

// VerifyChapter runs the active chapter's gates and applies the resulting
// lifecycle transition to durable saga state.
// VerifyChapter runs one chapter's gates and records the outcome in the saga
// state.
//
// The detector is a parameter rather than a fixed FileGateDetector so a caller
// can say which gates apply — a test, and one day a project that configures its
// own. It replaced a pre-computed []string: with the ports design the detector
// is the thing that decides, and passing both meant two sources of truth for
// one answer.
func VerifyChapter(ctx context.Context, verifier *ChapterVerifier, repoDir string, state *SagaState, chapterIndex int) error {
	if state == nil {
		return fmt.Errorf("saga: state is required")
	}
	if chapterIndex < 0 || chapterIndex >= len(state.Chapters) {
		return fmt.Errorf("saga: chapter index %d out of range", chapterIndex)
	}
	chapter := &state.Chapters[chapterIndex]
	if chapter.Status != StatusExecuting && chapter.Status != StatusVerifying {
		return fmt.Errorf("saga: chapter %d is %q, want executing or verifying", chapter.Number, chapter.Status)
	}
	if cancelErr := sagaCancellation(ctx, nil); cancelErr != nil {
		return cancelErr
	}
	if chapter.Status == StatusExecuting {
		if err := transitionChapter(chapter, StatusVerifying); err != nil {
			return err
		}
	}

	commit, err := verifyThroughPorts(ctx, verifier, repoDir, *chapter)
	if err != nil {
		if cancelErr := sagaCancellation(ctx, err); cancelErr != nil {
			// Verifying is an in-flight marker, not a resumable boundary. The
			// next wake must be able to retry the chapter without a strike.
			if chapter.Status == StatusVerifying {
				chapter.Status = StatusExecuting
			}
			return cancelErr
		}
		if strikeErr := RecordGateFailure(state); strikeErr != nil {
			return strikeErr
		}
		if transitionErr := transitionChapter(chapter, StatusFailed); transitionErr != nil {
			return transitionErr
		}
		chapter.Verification = err.Error()
		return err
	}

	if err := transitionChapter(chapter, StatusDone); err != nil {
		return err
	}
	if err := RecordChapterSuccess(state); err != nil {
		return err
	}
	chapter.Verification = "quality gates passed"
	chapter.Commit = commit
	return nil
}

// VerifyChapterAndPersist applies the chapter result and persists the updated
// saga artifact even when verification fails, preserving the failure/strike

func transitionChapter(chapter *Chapter, to ChapterStatus) error {
	if err := ValidateTransition(chapter.Status, to); err != nil {
		return err
	}
	chapter.Status = to
	return nil
}

// verifyThroughPorts adapts ChapterVerifier's result to the commit-or-error
// shape the lifecycle state machine works in.
//
// The verifier reports a failed chapter as a result rather than an error,
// because "the gates failed" is an outcome and not a malfunction. The state
// machine needs the distinction the other way round — it records a strike and
// a message — so the translation happens here rather than in either of them.
func verifyThroughPorts(ctx context.Context, verifier *ChapterVerifier, repoDir string, chapter Chapter) (string, error) {
	result, err := verifier.Verify(ctx, repoDir, chapter)
	if err != nil {
		return "", err
	}
	if result.Passed {
		return result.Commit, nil
	}
	var failed []string
	for _, run := range result.GateRuns {
		if !run.Passed {
			failed = append(failed, fmt.Sprintf("%s: %s", run.Gate.Name, strings.TrimSpace(run.Output)))
		}
	}
	if len(failed) == 0 {
		return "", fmt.Errorf("saga: quality gates failed")
	}
	return "", fmt.Errorf("saga: quality gates failed — %s", strings.Join(failed, "; "))
}
