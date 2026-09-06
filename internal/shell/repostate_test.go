package shell

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The branch and the count of changed paths from a real repository; a plain
// directory reports nothing rather than a guess.
func TestRepoStateReadsBranchAndDirtyCount(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "trunk")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", "a.txt")
	git("commit", "-q", "-m", "one")
	branch, dirty, ok := RepoState(context.Background(), dir)
	if !ok || branch != "trunk" || dirty != 0 {
		t.Fatalf("clean repo = %q %d %v", branch, dirty, ok)
	}
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("b"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "new.txt"), []byte("n"), 0o600)
	branch, dirty, ok = RepoState(context.Background(), dir)
	if !ok || branch != "trunk" || dirty != 2 {
		t.Fatalf("dirty repo = %q %d %v, want trunk 2", branch, dirty, ok)
	}
	if _, _, ok := RepoState(context.Background(), t.TempDir()); ok {
		t.Fatal("a plain directory reported git state")
	}
}
