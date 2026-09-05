package engine

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// realRunner runs saga commands through sh in the repository, the way the CLI
// adapter does, so the checkpointer's git is real git against a real tree.
type realRunner struct{}

func (realRunner) Run(ctx context.Context, command, dir string) (CommandResult, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	result := CommandResult{Output: string(out)}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			result.ExitCode = exit.ExitCode()
			result.Failure = strings.TrimSpace(string(out))
			if result.Failure == "" {
				result.Failure = err.Error()
			}
			return result, nil
		}
		return result, err
	}
	return result, nil
}

func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "--quiet", "--initial-branch=main")
	run("config", "user.email", "test@example.invalid")
	run("config", "user.name", "Test")
	writeFile(t, dir, "tracked.txt", "committed\n")
	writeFile(t, dir, "user.txt", "committed\n")
	run("add", "-A")
	run("commit", "--quiet", "-m", "first")
	return dir
}

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

// The user's own uncommitted work was there before the chapter began; the
// chapter's work -- edits, a new untracked file, a new staged file -- was not.
// A failed chapter's rollback removes the second and leaves the first alone.
func TestRollbackDiscardsTheChapterAndKeepsTheUsersOwnChanges(t *testing.T) {
	repo := gitRepo(t)
	writeFile(t, repo, "user.txt", "the user's uncommitted edit\n")
	writeFile(t, repo, "user-notes.txt", "the user's untracked notes\n")
	cp := NewCommandCheckpointer(context.Background(), realRunner{})
	mark, err := cp.MarkChapter(repo)
	if err != nil {
		t.Fatal(err)
	}

	// The chapter.
	writeFile(t, repo, "tracked.txt", "rewritten by the chapter\n")
	writeFile(t, repo, "new.txt", "created by the chapter\n")
	writeFile(t, repo, "staged.txt", "created and staged by the chapter\n")
	gitOut(t, repo, "add", "staged.txt")

	if err := cp.RollbackChapter(repo, &mark); err != nil {
		t.Fatalf("RollbackChapter: %v", err)
	}
	if got := readFile(t, repo, "user.txt"); got != "the user's uncommitted edit\n" {
		t.Errorf("user.txt = %q; the user's own edit was discarded", got)
	}
	if got := readFile(t, repo, "user-notes.txt"); got != "the user's untracked notes\n" {
		t.Errorf("user-notes.txt = %q; the user's own untracked file was removed", got)
	}
	if got := readFile(t, repo, "tracked.txt"); got != "committed\n" {
		t.Errorf("tracked.txt = %q; the chapter's edit survived", got)
	}
	for _, name := range []string{"new.txt", "staged.txt"} {
		if got := readFile(t, repo, name); got != "<missing>" {
			t.Errorf("%s survived the rollback: %q", name, got)
		}
	}
	if ls := gitOut(t, repo, "ls-files"); strings.Contains(ls, "staged.txt") {
		t.Errorf("staged.txt is still in the index:\n%s", ls)
	}
}
