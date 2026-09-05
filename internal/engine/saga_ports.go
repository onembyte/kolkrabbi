package engine

import (
	"context"
	"fmt"
	"github.com/onembyte/kolkrabbi/internal/shell"
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
// sagaArtifactName is the progress artifact the saga keeps in the repository.
const sagaArtifactName = "SAGA.md"

type commandCheckpointer struct {
	ctx    context.Context
	runner CommandRunner
}

// NewCommandCheckpointer returns a GitCheckpointer backed by a command runner.
func NewCommandCheckpointer(ctx context.Context, runner CommandRunner) GitCheckpointer {
	return commandCheckpointer{ctx: ctx, runner: runner}
}

// CommitChapter stages everything and commits it, returning the short hash.
func (c commandCheckpointer) CommitChapter(repoDir string, chapterNum int, summary string, mark *ChapterMark) (string, error) {
	message := shell.Quote(fmt.Sprintf("saga(chapter %d): %s", chapterNum, summary))
	add := "git add -A"
	if mark.overDirtyTree() {
		owned, err := c.chapterPaths(repoDir, mark)
		if err != nil {
			return "", err
		}
		// The artifact rides with every chapter commit, as it always has; a
		// path that did not change is a no-op to add.
		add = "git add -A -- " + quoteAll(append(owned, sagaArtifactName))
	}
	if err := c.mustRun(add+" && git commit -m "+message, repoDir, "commit"); err != nil {
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
func (c commandCheckpointer) MarkChapter(repoDir string) (ChapterMark, error) {
	// `git stash create` writes a commit object for the current worktree and
	// index without moving anything: the user's uncommitted edits, captured,
	// their history untouched. Empty output means a clean tree.
	snapshot, err := c.output("git stash create", repoDir, "mark")
	if err != nil {
		return ChapterMark{}, err
	}
	untracked, err := c.lines("git ls-files --others --exclude-standard", repoDir, "mark")
	if err != nil {
		return ChapterMark{}, err
	}
	head, err := c.output("git rev-parse HEAD", repoDir, "mark")
	if err != nil {
		return ChapterMark{}, err
	}
	return ChapterMark{Snapshot: strings.TrimSpace(snapshot), Untracked: untracked, Head: strings.TrimSpace(head)}, nil
}

// RollbackChapter puts the tree back to the mark: tracked files to their
// content at chapter start (the user's own edits included, the chapter's
// gone), files the chapter added to the index unstaged and removed, and
// untracked files that were not there at the mark removed. Without a mark it
// does the one thing that cannot destroy the user's files: tracked files back
// to HEAD, untracked files left where they are.
func (c commandCheckpointer) RollbackChapter(repoDir string, mark *ChapterMark) error {
	base := "HEAD"
	if mark != nil && mark.Snapshot != "" {
		if !isHex(mark.Snapshot) {
			return fmt.Errorf("saga: rollback mark %q is not a commit hash", mark.Snapshot)
		}
		base = mark.Snapshot
	}
	if err := c.mustRun("git checkout "+base+" -- .", repoDir, "rollback"); err != nil {
		return err
	}
	if mark == nil {
		return nil
	}
	indexed, err := c.lines("git ls-files", repoDir, "rollback")
	if err != nil {
		return err
	}
	inBase, err := c.lines("git ls-tree -r --name-only "+base, repoDir, "rollback")
	if err != nil {
		return err
	}
	// The saga's own artifact is never a chapter's file: it is written after
	// the mark on the wake that creates it, and a rollback that removed it would
	// erase the record of the rollback.
	if added := subtract(subtract(indexed, inBase), []string{sagaArtifactName}); len(added) > 0 {
		quoted := quoteAll(added)
		if err := c.mustRun("git rm -q --cached -- "+quoted+" && rm -f -- "+quoted, repoDir, "rollback"); err != nil {
			return err
		}
	}
	untrackedNow, err := c.lines("git ls-files --others --exclude-standard", repoDir, "rollback")
	if err != nil {
		return err
	}
	if created := subtract(subtract(untrackedNow, mark.Untracked), []string{sagaArtifactName}); len(created) > 0 {
		if err := c.mustRun("rm -rf -- "+quoteAll(created), repoDir, "rollback"); err != nil {
			return err
		}
	}
	return nil
}

func (c commandCheckpointer) output(command, repoDir, what string) (string, error) {
	result, err := c.runner.Run(c.ctx, command, repoDir)
	if err != nil {
		return "", fmt.Errorf("saga: %s could not run: %w", what, err)
	}
	if result.Failure != "" || result.ExitCode != 0 {
		return "", fmt.Errorf("saga: %s failed: %s", what, result.Failure)
	}
	return result.Output, nil
}

func (c commandCheckpointer) lines(command, repoDir, what string) ([]string, error) {
	out, err := c.output(command, repoDir, what)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines, nil
}

// subtract returns the members of have that are not in except, in order.
func subtract(have, except []string) []string {
	skip := make(map[string]bool, len(except))
	for _, e := range except {
		skip[e] = true
	}
	var out []string
	for _, h := range have {
		if !skip[h] {
			out = append(out, h)
		}
	}
	return out
}

func quoteAll(paths []string) string {
	quoted := make([]string, len(paths))
	for i, p := range paths {
		quoted[i] = shell.Quote(p)
	}
	return strings.Join(quoted, " ")
}

// HasChanges reports whether the working tree has anything to commit.
// overDirtyTree reports that the mark was taken over a tree that already had
// uncommitted work. Over a clean tree everything dirty afterwards is the
// chapter's, and the whole-tree commands are exactly right.
func (m *ChapterMark) overDirtyTree() bool {
	return m != nil && (m.Snapshot != "" || len(m.Untracked) > 0)
}

// chapterPaths lists what the chapter changed: tracked paths that differ from
// the mark's snapshot (edits, deletions, and files it added to the index) and
// untracked files the mark did not list. The user's pre-existing dirty files
// are in the snapshot as they were, so an unchanged one does not appear.
func (c commandCheckpointer) chapterPaths(repoDir string, mark *ChapterMark) ([]string, error) {
	base := "HEAD"
	if mark.Snapshot != "" {
		if !isHex(mark.Snapshot) {
			return nil, fmt.Errorf("saga: chapter mark %q is not a commit hash", mark.Snapshot)
		}
		base = mark.Snapshot
	}
	changed, err := c.lines("git diff --name-only "+base, repoDir, "commit")
	if err != nil {
		return nil, err
	}
	untracked, err := c.lines("git ls-files --others --exclude-standard", repoDir, "commit")
	if err != nil {
		return nil, err
	}
	return append(changed, subtract(untracked, mark.Untracked)...), nil
}

func (c commandCheckpointer) HasChanges(repoDir string, mark *ChapterMark) (bool, error) {
	if mark.overDirtyTree() {
		owned, err := c.chapterPaths(repoDir, mark)
		if err != nil {
			return false, err
		}
		return len(owned) > 0, nil
	}
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

// isHex accepts the one shape a snapshot may have: a git object hash. HEAD is
// the only other base and is a constant, so neither needs quoting.
func isHex(s string) bool {
	if len(s) < 7 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// HeadMoved reports that the repository's HEAD is no longer the commit the
// mark was taken on -- the chapter, or something else, has been committed since.
func (c commandCheckpointer) HeadMoved(repoDir string, mark *ChapterMark) (bool, error) {
	if mark == nil || mark.Head == "" {
		return false, nil
	}
	head, err := c.output("git rev-parse HEAD", repoDir, "resume")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(head) != mark.Head, nil
}
