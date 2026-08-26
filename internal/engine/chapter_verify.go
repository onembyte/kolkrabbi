package engine

import "fmt"

// ChapterVerifier orchestrates the verify-then-checkpoint cycle for a single
// saga chapter. It depends only on engine ports (QualityGateRunner,
// GitCheckpointer, QualityGateDetector), never on shell or platform packages.
type ChapterVerifier struct {
	Detector     QualityGateDetector
	Runner       QualityGateRunner
	Checkpointer GitCheckpointer
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
func (cv *ChapterVerifier) Verify(repoDir string, chapter Chapter) (*VerifyResult, error) {
	// If no changes were made, skip verification and commit.
	hasChanges, err := cv.Checkpointer.HasChanges(repoDir)
	if err != nil {
		return nil, fmt.Errorf("checking for changes: %w", err)
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
		commit, err := cv.Checkpointer.CommitChapter(repoDir, chapter.Number, chapter.Title)
		if err != nil {
			return nil, fmt.Errorf("committing chapter %d: %w", chapter.Number, err)
		}
		return &VerifyResult{
			Passed: true,
			Commit: commit,
		}, nil
	}

	// Run gates.
	results := cv.Runner.RunGates(repoDir, gates)

	allPassed := true
	for _, r := range results {
		if !r.Passed {
			allPassed = false
			break
		}
	}

	if allPassed {
		commit, err := cv.Checkpointer.CommitChapter(repoDir, chapter.Number, chapter.Title)
		if err != nil {
			return nil, fmt.Errorf("committing chapter %d: %w", chapter.Number, err)
		}
		return &VerifyResult{
			Passed:   true,
			Commit:   commit,
			GateRuns: results,
		}, nil
	}

	// Gates failed — rollback.
	if err := cv.Checkpointer.RollbackChapter(repoDir); err != nil {
		return nil, fmt.Errorf("rolling back chapter %d: %w", chapter.Number, err)
	}

	return &VerifyResult{
		Passed:   false,
		Strikes:  1,
		GateRuns: results,
	}, nil
}
