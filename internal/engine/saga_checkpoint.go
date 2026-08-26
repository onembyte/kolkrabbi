package engine

import (
	"context"
	"fmt"
	"strings"
)

// CommandResult is the engine-facing result of a project command.
type CommandResult struct {
	Output   string
	ExitCode int
	Failure  string
}

// CommandRunner is implemented by the platform shell adapter. Keeping this
// port small prevents saga orchestration from importing the platform layer.
type CommandRunner interface {
	Run(context.Context, string, string) (CommandResult, error)
}

// VerifyAndCommit runs every quality gate in repoDir and creates the chapter
// commit only when all gates succeed. A failed gate restores the worktree and
// returns the gate output to the caller.
func VerifyAndCommit(ctx context.Context, runner CommandRunner, repoDir string, gates []string, chapter Chapter) error {
	_, err := VerifyAndCommitResult(ctx, runner, repoDir, gates, chapter)
	return err
}

// VerifyAndCommitResult is VerifyAndCommit with the committed revision.
func VerifyAndCommitResult(ctx context.Context, runner CommandRunner, repoDir string, gates []string, chapter Chapter) (string, error) {
	if runner == nil {
		return "", fmt.Errorf("saga: command runner is required")
	}
	for _, gate := range gates {
		result, err := runner.Run(ctx, gate, repoDir)
		if err != nil {
			return "", gateFailure(ctx, runner, repoDir, fmt.Errorf("saga: quality gate %q could not run: %w", gate, err))
		}
		if result.Failure != "" || result.ExitCode != 0 {
			if result.Failure != "" {
				return "", gateFailure(ctx, runner, repoDir, fmt.Errorf("saga: quality gate %q failed: %s", gate, result.Failure))
			}
			return "", gateFailure(ctx, runner, repoDir, fmt.Errorf("saga: quality gate %q failed with exit code %d", gate, result.ExitCode))
		}
	}

	if result, err := runner.Run(ctx, "git add -A && git commit -m "+quoteCommitMessage(chapter), repoDir); err != nil {
		return "", fmt.Errorf("saga: commit could not run: %w", err)
	} else if result.Failure != "" || result.ExitCode != 0 {
		return "", fmt.Errorf("saga: commit failed: %s", result.Failure)
	}
	result, err := runner.Run(ctx, "git rev-parse --short HEAD", repoDir)
	if err != nil {
		return "", fmt.Errorf("saga: commit identity could not run: %w", err)
	}
	if result.Failure != "" || result.ExitCode != 0 {
		return "", fmt.Errorf("saga: commit identity failed: %s", result.Failure)
	}
	commit := strings.TrimSpace(result.Output)
	if commit == "" {
		return "", fmt.Errorf("saga: commit identity was empty")
	}
	return commit, nil
}

func gateFailure(ctx context.Context, runner CommandRunner, repoDir string, cause error) error {
	result, err := runner.Run(ctx, "git checkout -- .", repoDir)
	if err != nil {
		return fmt.Errorf("%w; rollback could not run: %v", cause, err)
	}
	if result.Failure != "" || result.ExitCode != 0 {
		return fmt.Errorf("%w; rollback failed: %s", cause, result.Failure)
	}
	return cause
}

func quoteCommitMessage(chapter Chapter) string {
	message := fmt.Sprintf("saga(chapter %d): %s", chapter.Number, chapter.Title)
	quoted := "'"
	for _, r := range message {
		if r == '\'' {
			quoted += "'\\''"
		} else {
			quoted += string(r)
		}
	}
	return quoted + "'"
}
