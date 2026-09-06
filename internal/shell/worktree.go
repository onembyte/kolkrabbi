package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// The git plumbing under plan 36: a subagent that writes files gets a worktree
// of its own, seeded with what the user has not committed, and what it did
// there comes back as a patch and lands in the user's tree. Everything here
// runs git; nothing here decides. The engine decides, through a port, and
// never sees a process.
//
// Two promises hold throughout. The user's index, branch, stash stack and
// reflog are never touched: a patch is taken through a throwaway index file,
// and applied to the working tree alone. And nothing is forced: a patch that
// does not fit is refused with git's own words, which name the file.

// worktreeDeadline bounds one git call. Adding a worktree checks out a whole
// tree, which on a large repository is the slowest thing here.
const worktreeDeadline = 2 * time.Minute

// gitIdentity lets the seed commit happen on a machine where git has no
// user.name; it is kolk's commit in kolk's worktree, never the user's.
var gitIdentity = []string{
	"GIT_AUTHOR_NAME=kolk", "GIT_AUTHOR_EMAIL=kolk@localhost",
	"GIT_COMMITTER_NAME=kolk", "GIT_COMMITTER_EMAIL=kolk@localhost",
}

// git runs one git command in dir with stdin, returning stdout, and on failure
// an error carrying git's stderr trimmed to one line group. extra is appended
// to the environment.
func git(ctx context.Context, dir string, stdin []byte, extra []string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, worktreeDeadline)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = inheritedEnv(extra)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("git %s: did not finish within %s", args[0], worktreeDeadline)
		}
		msg := strings.TrimSpace(errOut.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("git %s: %s", args[0], msg)
	}
	return out.Bytes(), nil
}

// TreePatch is the difference between a working tree and its HEAD — staged,
// unstaged and untracked, as one binary patch — with the ignored files left
// out. It stages nothing: the tree is added to a throwaway index that is
// deleted before this returns, so the user's own index is exactly as it was.
//
// A repository with no commit has no HEAD to differ from, and a plain
// directory has nothing at all; both are errors, and the caller's answer to
// either is to not isolate.
func TreePatch(ctx context.Context, dir string) ([]byte, error) {
	index, err := os.CreateTemp("", "kolk-index-*")
	if err != nil {
		return nil, err
	}
	indexPath := index.Name()
	_ = index.Close()
	// git wants to create the index itself; an empty existing file reads as a
	// corrupt one.
	_ = os.Remove(indexPath)
	defer func() { _ = os.Remove(indexPath) }()
	env := []string{"GIT_INDEX_FILE=" + indexPath}
	if _, err := git(ctx, dir, nil, env, "add", "-A", "--", "."); err != nil {
		return nil, err
	}
	patch, err := git(ctx, dir, nil, env, "diff", "--cached", "--binary", "--no-color", "--no-ext-diff", "HEAD", "--")
	if err != nil {
		return nil, err
	}
	return patch, nil
}

// ApplyPatch applies a patch from TreePatch to a working tree, and only the
// working tree: the index is not staged, so what lands looks to the user like
// an edit they have not yet added. An empty patch is nothing to do. A patch
// that does not fit is refused as a whole — git applies none of it — with the
// file named in the error.
func ApplyPatch(ctx context.Context, dir string, patch []byte) error {
	if len(bytes.TrimSpace(patch)) == 0 {
		return nil
	}
	_, err := git(ctx, dir, patch, nil, "apply", "--whitespace=nowarn", "--binary", "-")
	return err
}

// AddWorktree checks out a detached worktree of repo's HEAD at dir, applies
// the seed patch and commits it there, so that TreePatch of the worktree
// later reports the work done in it and not the seed again. The commit is on
// a detached HEAD in the worktree alone; no branch of the user's moves.
//
// dir must not exist. A failure after the checkout removes what was made:
// a half-seeded worktree is worse than none, because a subagent in it would
// not see what the user sees.
func AddWorktree(ctx context.Context, repo, dir string, seed []byte) error {
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("worktree %s already exists", dir)
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o750); err != nil {
		return err
	}
	if _, err := git(ctx, repo, nil, nil, "worktree", "add", "--detach", "--quiet", dir, "HEAD"); err != nil {
		return err
	}
	if len(bytes.TrimSpace(seed)) == 0 {
		return nil
	}
	if err := seedWorktree(ctx, dir, seed); err != nil {
		_ = RemoveWorktree(ctx, repo, dir)
		return err
	}
	return nil
}

func seedWorktree(ctx context.Context, dir string, seed []byte) error {
	if err := ApplyPatch(ctx, dir, seed); err != nil {
		return fmt.Errorf("seeding the worktree: %w", err)
	}
	if _, err := git(ctx, dir, nil, nil, "add", "-A", "--", "."); err != nil {
		return err
	}
	_, err := git(ctx, dir, nil, gitIdentity, "commit", "--quiet", "--no-verify", "--allow-empty",
		"-m", "kolk: the user's uncommitted tree, as this task found it")
	return err
}

// RemoveWorktree deletes a worktree and its registration in repo. Force is
// deliberate: the worktree's work has either landed or been kept as a patch
// by the time this runs, and a dirty worktree left behind is a leak. A
// directory that is already gone is pruned rather than reported.
func RemoveWorktree(ctx context.Context, repo, dir string) error {
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		_, err := git(ctx, repo, nil, nil, "worktree", "prune")
		return err
	}
	_, err := git(ctx, repo, nil, nil, "worktree", "remove", "--force", dir)
	return err
}
