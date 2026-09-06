package shell

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// testRepo is a real repository with one commit and a .gitignore, because the
// worktree plumbing is only worth testing against git itself.
func testRepo(t *testing.T) (dir string, git func(args ...string) string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir = t.TempDir()
	git = func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	git("init", "-q", "-b", "trunk")
	write(t, filepath.Join(dir, "a.txt"), "a\n")
	write(t, filepath.Join(dir, ".gitignore"), "build/\n")
	git("add", "-A")
	git("commit", "-q", "-m", "one")
	return dir, git
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

// A worktree seeded from the user's tree sees what the user sees: the
// uncommitted edit, the untracked file, and not the ignored one. What the
// subagent then does in it comes back as a patch of its own work alone, and
// lands in the user's tree without touching the user's index.
func TestAWorktreeCarriesTheUncommittedTreeAndItsWorkLandsBack(t *testing.T) {
	repo, git := testRepo(t)
	ctx := context.Background()
	write(t, filepath.Join(repo, "a.txt"), "edited\n")
	write(t, filepath.Join(repo, "new.txt"), "new\n")
	write(t, filepath.Join(repo, "build", "out.bin"), "ignored\n")

	seed, err := TreePatch(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(seed), "new.txt") || strings.Contains(string(seed), "out.bin") {
		t.Fatalf("seed patch should carry the untracked file and not the ignored one:\n%s", seed)
	}
	if out := git("diff", "--cached", "--stat"); strings.TrimSpace(out) != "" {
		t.Fatalf("TreePatch touched the user's index:\n%s", out)
	}

	wt := filepath.Join(t.TempDir(), "task-1")
	if err := AddWorktree(ctx, repo, wt, seed); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(wt, "a.txt")); got != "edited\n" {
		t.Fatalf("worktree a.txt = %q, want the uncommitted edit", got)
	}
	if got := read(t, filepath.Join(wt, "new.txt")); got != "new\n" {
		t.Fatalf("worktree new.txt = %q, want the untracked file", got)
	}
	if read(t, filepath.Join(wt, "build", "out.bin")) != "<missing>" {
		t.Fatal("an ignored file reached the worktree")
	}

	// The subagent's work: one edit, one new file, one deletion.
	write(t, filepath.Join(wt, "new.txt"), "new, then more\n")
	write(t, filepath.Join(wt, "c.txt"), "c\n")
	if err := os.Remove(filepath.Join(wt, "a.txt")); err != nil {
		t.Fatal(err)
	}
	work, err := TreePatch(ctx, wt)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(work), "+edited") {
		t.Fatalf("the work patch repeats the seed, so landing it would fail:\n%s", work)
	}
	if err := ApplyPatch(ctx, repo, work); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(repo, "new.txt")); got != "new, then more\n" {
		t.Fatalf("landed new.txt = %q", got)
	}
	if got := read(t, filepath.Join(repo, "c.txt")); got != "c\n" {
		t.Fatalf("landed c.txt = %q", got)
	}
	if read(t, filepath.Join(repo, "a.txt")) != "<missing>" {
		t.Fatal("the deletion did not land")
	}
	if out := git("diff", "--cached", "--stat"); strings.TrimSpace(out) != "" {
		t.Fatalf("landing touched the user's index:\n%s", out)
	}
	if out := git("rev-parse", "--abbrev-ref", "HEAD"); strings.TrimSpace(out) != "trunk" {
		t.Fatalf("the user's branch moved: %s", out)
	}

	if err := RemoveWorktree(ctx, repo, wt); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatal("the worktree directory is still there")
	}
	if out := git("worktree", "list"); strings.Contains(out, "task-1") {
		t.Fatalf("git still lists the worktree:\n%s", out)
	}
}

// A patch that does not fit is refused, never forced, and the refusal names
// the file so the run can say which earlier task it collided with.
func TestApplyPatchRefusesAConflictAndNamesTheFile(t *testing.T) {
	repo, _ := testRepo(t)
	ctx := context.Background()
	wt := filepath.Join(t.TempDir(), "task-2")
	if err := AddWorktree(ctx, repo, wt, nil); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(wt, "a.txt"), "from the worktree\n")
	work, err := TreePatch(ctx, wt)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(repo, "a.txt"), "from an earlier task\n")
	err = ApplyPatch(ctx, repo, work)
	if err == nil || !strings.Contains(err.Error(), "a.txt") {
		t.Fatalf("conflict = %v, want a refusal naming a.txt", err)
	}
	if got := read(t, filepath.Join(repo, "a.txt")); got != "from an earlier task\n" {
		t.Fatalf("a refused patch changed the tree: %q", got)
	}
}

// An empty patch is nothing to apply, and a plain directory is nothing to
// isolate: both are answered, neither runs git against the wrong place.
func TestWorktreePlumbingRefusesWhatIsNotARepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	ctx := context.Background()
	plain := t.TempDir()
	if _, err := TreePatch(ctx, plain); err == nil {
		t.Fatal("a plain directory produced a patch")
	}
	if err := AddWorktree(ctx, plain, filepath.Join(t.TempDir(), "wt"), nil); err == nil {
		t.Fatal("a plain directory grew a worktree")
	}
	if err := ApplyPatch(ctx, plain, nil); err != nil {
		t.Fatalf("an empty patch should be nothing to do, got %v", err)
	}
}

// The isolator the engine is wired with: a tree per task under the data
// directory, work landed back, a patch that will not fit kept on disk and
// named in the refusal, and a plain directory refused before any git runs.
func TestTheWorktreeIsolatorIsolatesLandsAndKeepsARefusedPatch(t *testing.T) {
	repo, git := testRepo(t)
	ctx := context.Background()
	store := filepath.Join(t.TempDir(), "worktrees")
	iso := NewWorktreeIsolator(store)

	write(t, filepath.Join(repo, "a.txt"), "edited\n")
	dir, err := iso.Isolate(ctx, repo, "t_one")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(dir, store) {
		t.Fatalf("tree %q is not under the store %q", dir, store)
	}
	if got := read(t, filepath.Join(dir, "a.txt")); got != "edited\n" {
		t.Fatalf("the tree did not carry the uncommitted edit: %q", got)
	}
	write(t, filepath.Join(dir, "b.txt"), "b\n")
	if err := iso.Land(ctx, repo, dir); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(repo, "b.txt")); got != "b\n" {
		t.Fatalf("landed b.txt = %q", got)
	}
	iso.Release(ctx, repo, dir)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("the tree is still there after Release")
	}
	if out := git("worktree", "list"); strings.Contains(out, "t_one") {
		t.Fatalf("git still lists the tree:\n%s", out)
	}

	// A collision: the tree edits a.txt one way, the user's tree has moved
	// another way since. The landing is refused and the work is kept.
	dir, err = iso.Isolate(ctx, repo, "t_two")
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "a.txt"), "from the tree\n")
	write(t, filepath.Join(repo, "a.txt"), "moved since\n")
	err = iso.Land(ctx, repo, dir)
	if err == nil || !strings.Contains(err.Error(), "a.txt") || !strings.Contains(err.Error(), "kept at ") {
		t.Fatalf("refused landing = %v, want a.txt named and the patch kept", err)
	}
	kept := strings.TrimSpace(err.Error()[strings.LastIndex(err.Error(), "kept at ")+len("kept at "):])
	if got := read(t, kept); !strings.Contains(got, "from the tree") {
		t.Fatalf("the kept patch at %q does not carry the work: %q", kept, got)
	}
	iso.Release(ctx, repo, dir)

	if _, err := iso.Isolate(ctx, t.TempDir(), "t_three"); err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("a plain directory was isolated, or refused for the wrong reason: %v", err)
	}
}
