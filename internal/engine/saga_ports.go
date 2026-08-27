package engine

import (
	"context"
	"fmt"
	"strings"
)

// The saga's quality gates and git checkpoints, expressed over the one command
// port the engine already has.
//
// These are the implementations the ports design was missing: ChapterVerifier
// was written against QualityGateRunner and GitCheckpointer, only fakes ever
// satisfied them, and a second ad-hoc implementation of the same behaviour grew
// beside it and was the one wired up. This is that behaviour, moved behind the
// ports it should always have been behind.
//
// Both carry a context. The ports were declared without one, which would have
// meant a quality gate nobody could interrupt — and `kolk saga stop` exists, so
// a `make check` that runs for two minutes has to be stoppable.

// commandGateRunner runs quality gates through a CommandRunner.
type commandGateRunner struct {
	ctx    context.Context
	runner CommandRunner
}

// NewCommandGateRunner returns a QualityGateRunner backed by a command runner.
func NewCommandGateRunner(ctx context.Context, runner CommandRunner) QualityGateRunner {
	return commandGateRunner{ctx: ctx, runner: runner}
}

// RunGates runs every gate and reports each one separately.
//
// Every gate runs even after one fails. Stopping at the first failure hides the
// rest, and naming gates individually is pointless if a run only ever tells you
// about the first broken one.
func (g commandGateRunner) RunGates(repoDir string, gates []QualityGate) []GateResult {
	results := make([]GateResult, 0, len(gates))
	for _, gate := range gates {
		if err := g.ctx.Err(); err != nil {
			results = append(results, GateResult{Gate: gate, Output: "not run: " + err.Error()})
			continue
		}
		result, err := g.runner.Run(g.ctx, gate.Command, repoDir)
		switch {
		case err != nil:
			// A gate that could not run is a gate that did not pass. Treating
			// it as success is how a broken toolchain becomes a green saga.
			results = append(results, GateResult{Gate: gate, Output: err.Error()})
		case result.Failure != "" || result.ExitCode != 0:
			results = append(results, GateResult{Gate: gate, Output: gateOutput(result)})
		default:
			results = append(results, GateResult{Gate: gate, Passed: true, Output: result.Output})
		}
	}
	return results
}

func gateOutput(result CommandResult) string {
	if strings.TrimSpace(result.Output) == "" {
		return result.Failure
	}
	if result.Failure == "" {
		return result.Output
	}
	return result.Output + "\n" + result.Failure
}

// commandCheckpointer makes saga commits through a CommandRunner.
type commandCheckpointer struct {
	ctx    context.Context
	runner CommandRunner
}

// NewCommandCheckpointer returns a GitCheckpointer backed by a command runner.
func NewCommandCheckpointer(ctx context.Context, runner CommandRunner) GitCheckpointer {
	return commandCheckpointer{ctx: ctx, runner: runner}
}

// CommitChapter stages everything and commits it, returning the short hash.
func (c commandCheckpointer) CommitChapter(repoDir string, chapterNum int, summary string) (string, error) {
	message := shellQuote(fmt.Sprintf("saga(chapter %d): %s", chapterNum, summary))
	if err := c.mustRun("git add -A && git commit -m "+message, repoDir, "commit"); err != nil {
		return "", err
	}

	result, err := c.runner.Run(c.ctx, "git rev-parse --short HEAD", repoDir)
	if err != nil {
		return "", fmt.Errorf("saga: commit identity could not run: %w", err)
	}
	if result.Failure != "" || result.ExitCode != 0 {
		return "", fmt.Errorf("saga: commit identity failed: %s", result.Failure)
	}
	commit := strings.TrimSpace(result.Output)
	if commit == "" {
		// Recording a chapter as committed at revision "" is worse than
		// failing: it reads as done and cannot be rewound to.
		return "", fmt.Errorf("saga: commit identity was empty")
	}
	return commit, nil
}

// RollbackChapter discards everything uncommitted.
func (c commandCheckpointer) RollbackChapter(repoDir string) error {
	return c.mustRun("git checkout -- .", repoDir, "rollback")
}

// HasChanges reports whether the working tree has anything to commit.
func (c commandCheckpointer) HasChanges(repoDir string) (bool, error) {
	result, err := c.runner.Run(c.ctx, "git status --porcelain", repoDir)
	if err != nil {
		return false, fmt.Errorf("saga: reading the worktree could not run: %w", err)
	}
	if result.Failure != "" || result.ExitCode != 0 {
		return false, fmt.Errorf("saga: reading the worktree failed: %s", result.Failure)
	}
	return strings.TrimSpace(result.Output) != "", nil
}

func (c commandCheckpointer) mustRun(command, repoDir, what string) error {
	result, err := c.runner.Run(c.ctx, command, repoDir)
	if err != nil {
		return fmt.Errorf("saga: %s could not run: %w", what, err)
	}
	if result.Failure != "" || result.ExitCode != 0 {
		return fmt.Errorf("saga: %s failed: %s", what, result.Failure)
	}
	return nil
}

// shellQuote wraps text for a POSIX shell.
//
// A chapter title is model-written text arriving on a command line, so this is
// the boundary where a quote in a title stops being punctuation.
func shellQuote(text string) string {
	return "'" + strings.ReplaceAll(text, "'", `'\''`) + "'"
}
