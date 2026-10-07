package shell

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WorktreeIsolator is the engine's Isolator (plan 36) over the git plumbing
// in this file's neighbour: a detached worktree per task under one store
// directory, outside every project. It holds no state but the store; every
// answer comes from git at the time of asking.
type WorktreeIsolator struct {
	store string
}

// NewWorktreeIsolator makes an isolator whose trees live under store, which
// need not exist yet.
func NewWorktreeIsolator(store string) WorktreeIsolator {
	return WorktreeIsolator{store: store}
}

// Isolate makes a tree for one task, seeded with root's uncommitted state,
// and returns it. A root that is not a repository, or has no commit to
// branch from, is refused with a reason the status row can show; the caller
// then runs the task in the shared tree.
func (w WorktreeIsolator) Isolate(ctx context.Context, root, name string) (string, error) {
	if _, _, ok := RepoState(ctx, root); !ok {
		return "", errors.New("not a git repository, or git is not installed")
	}
	seed, err := TreePatch(ctx, root)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(w.store, name)
	if err := AddWorktree(ctx, root, dir, seed); err != nil {
		return "", err
	}
	return dir, nil
}

// Resume admits only the saved task's exact directory, still registered in
// this repository. Resolving the store permits normal platform aliases (such
// as /var on macOS), but replacing the task directory with a symlink does not.
func (w WorktreeIsolator) Resume(ctx context.Context, root, name, dir string) error {
	if name == "" || name == "." || filepath.Base(name) != name {
		return errors.New("invalid saved worktree identity")
	}
	store, err := filepath.EvalSymlinks(w.store)
	if err != nil {
		return fmt.Errorf("saved worktree store: %w", err)
	}
	store, err = filepath.Abs(store)
	if err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("saved worktree %s is missing or replaced", dir)
	}
	actual, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	actual, err = filepath.Abs(actual)
	if err != nil || actual != filepath.Join(store, name) {
		return errors.New("saved worktree is outside this task's directory")
	}
	common := func(path string) (string, error) {
		out, err := git(ctx, path, nil, nil, "rev-parse", "--path-format=absolute", "--git-common-dir")
		if err != nil {
			return "", err
		}
		return filepath.EvalSymlinks(strings.TrimSpace(string(out)))
	}
	rootCommon, err := common(root)
	if err != nil {
		return err
	}
	taskCommon, err := common(actual)
	if err != nil || taskCommon != rootCommon {
		return errors.New("saved worktree belongs to a different repository")
	}
	listed, err := git(ctx, root, nil, nil, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return err
	}
	for _, field := range strings.Split(string(listed), "\x00") {
		if strings.HasPrefix(field, "worktree ") {
			path, err := filepath.EvalSymlinks(strings.TrimPrefix(field, "worktree "))
			if err == nil && path == actual {
				return nil
			}
		}
	}
	return errors.New("saved worktree is no longer registered in this project")
}

// Land applies what happened in dir to root. A patch that does not fit is
// refused as a whole; the work is kept beside the store as a patch file, and
// the refusal names both the file git stopped at and where the work is.
func (w WorktreeIsolator) Land(ctx context.Context, root, dir string) error {
	patch, err := TreePatch(ctx, dir)
	if err != nil {
		return err
	}
	if err := ApplyPatch(ctx, root, patch); err != nil {
		kept := filepath.Join(w.store, filepath.Base(dir)+".patch")
		if writeErr := os.WriteFile(kept, patch, 0o600); writeErr != nil {
			return fmt.Errorf("%w; and the work could not be kept: %w", err, writeErr)
		}
		return fmt.Errorf("%w; the task's work is kept at %s", err, kept)
	}
	return nil
}

// Release removes the tree. There is nothing to report: the work has landed
// or been kept by the time this runs, and a removal that fails leaves a
// directory git will prune on the next add.
func (w WorktreeIsolator) Release(ctx context.Context, root, dir string) {
	_ = RemoveWorktree(ctx, root, dir)
}
