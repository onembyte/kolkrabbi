package engine

import (
	"context"
	"fmt"
)

// VerifyChapter runs the active chapter's gates and applies the resulting
// lifecycle transition to durable saga state.
func VerifyChapter(ctx context.Context, runner CommandRunner, repoDir string, state *SagaState, chapterIndex int, gates []string) error {
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
	if chapter.Status == StatusExecuting {
		if err := transitionChapter(chapter, StatusVerifying); err != nil {
			return err
		}
	}

	commit, err := VerifyAndCommitResult(ctx, runner, repoDir, gates, *chapter)
	if err != nil {
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
// state needed for resume.
func VerifyChapterAndPersist(ctx context.Context, runner CommandRunner, repoDir string, state *SagaState, chapterIndex int, gates []string, write ArtifactWriter) error {
	verifyErr := VerifyChapter(ctx, runner, repoDir, state, chapterIndex, gates)
	artifactErr := SaveSagaArtifact(repoDir, state, write)
	if artifactErr != nil {
		if verifyErr != nil {
			return fmt.Errorf("%v; persisting saga artifact: %w", verifyErr, artifactErr)
		}
		return fmt.Errorf("saga: persisting saga artifact: %w", artifactErr)
	}
	return verifyErr
}

func transitionChapter(chapter *Chapter, to ChapterStatus) error {
	if err := ValidateTransition(chapter.Status, to); err != nil {
		return err
	}
	chapter.Status = to
	return nil
}
