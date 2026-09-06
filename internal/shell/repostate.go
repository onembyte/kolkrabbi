package shell

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"
)

// RepoState is what source control says about a working tree: the branch
// and how many paths differ from HEAD (staged, unstaged and untracked). ok is
// false where git is absent, the directory is not a repository, or git did
// not answer within the deadline — a view that asks this per card must never
// hang on one of them.
func RepoState(ctx context.Context, dir string) (branch string, dirty int, ok bool) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	run := func(args ...string) (string, bool) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
		cmd.Env = inheritedEnv(nil)
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err != nil {
			return "", false
		}
		return out.String(), true
	}
	head, ok := run("rev-parse", "--abbrev-ref", "HEAD")
	if !ok {
		return "", 0, false
	}
	status, ok := run("status", "--porcelain", "--untracked-files=normal")
	if !ok {
		return "", 0, false
	}
	branch = strings.TrimSpace(head)
	for _, line := range strings.Split(status, "\n") {
		if strings.TrimSpace(line) != "" {
			dirty++
		}
	}
	return branch, dirty, true
}
