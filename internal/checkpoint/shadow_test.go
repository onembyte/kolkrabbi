package checkpoint

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitOr(t *testing.T, skip string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip(skip)
	}
}

// newProject builds a real git repository with one committed file.
func newProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "--quiet", "--initial-branch=main"},
		{"config", "user.email", "test@example.invalid"},
		{"config", "user.name", "Test"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "kept.txt"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "--quiet", "-m", "first"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return dir
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

func TestShadowSnapshotsTheWholeTreeIncludingWhatBashChanged(t *testing.T) {
	gitOr(t, "git is required for the shadow store")
	project := newProject(t)

	shadow, err := OpenShadow(context.Background(), t.TempDir(), project)
	if err != nil {
		t.Fatalf("OpenShadow: %v", err)
	}
	if _, err := shadow.Snapshot(context.Background(), 1); err != nil {
		t.Fatalf("first snapshot: %v", err)
	}

	// The change kolk never saw: no write_file, no edit_file, just a file on
	// disk that is different now.
	if err := os.WriteFile(filepath.Join(project, "kept.txt"), []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "made-by-bash.txt"), []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	changed, err := shadow.ChangedSinceSnapshot(context.Background())
	if err != nil {
		t.Fatalf("ChangedSinceSnapshot: %v", err)
	}
	joined := strings.Join(changed, " ")
	for _, want := range []string{"kept.txt", "made-by-bash.txt"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the shadow store did not notice %s: %v", want, changed)
		}
	}
}

// The property the whole design exists to protect. If this test ever fails,
// kolk is writing into someone's repository behind their back.
func TestShadowNeverTouchesTheUsersOwnGitState(t *testing.T) {
	gitOr(t, "git is required for the shadow store")
	project := newProject(t)

	before := gitOut(t, project, "status", "--porcelain=v1", "--branch")
	beforeLog := gitOut(t, project, "reflog", "--all")
	beforeStash := gitOut(t, project, "stash", "list")

	shadow, err := OpenShadow(context.Background(), t.TempDir(), project)
	if err != nil {
		t.Fatalf("OpenShadow: %v", err)
	}
	for turn := 1; turn <= 3; turn++ {
		if err := os.WriteFile(filepath.Join(project, "kept.txt"),
			[]byte(strings.Repeat("x", turn)), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := shadow.Snapshot(context.Background(), turn); err != nil {
			t.Fatalf("snapshot %d: %v", turn, err)
		}
	}

	if got := gitOut(t, project, "reflog", "--all"); got != beforeLog {
		t.Errorf("the user's reflog moved:\nbefore %q\nafter  %q", beforeLog, got)
	}
	if got := gitOut(t, project, "stash", "list"); got != beforeStash {
		t.Errorf("the user's stash stack changed:\nbefore %q\nafter %q", beforeStash, got)
	}
	// status differs only by the edit the test itself made, never by an index entry.
	after := gitOut(t, project, "status", "--porcelain=v1", "--branch")
	if strings.Contains(after, "M  kept.txt") {
		t.Errorf("the shadow store staged a change in the user's index: %q", after)
	}
	if before == after && !strings.Contains(after, "kept.txt") {
		t.Errorf("the test never dirtied the tree, so it proves nothing: %q", after)
	}
}

// Blob reuse is what makes a snapshot of a large checkout cheap. Without the
// alternates file the store would copy the tree on every first snapshot.
func TestShadowReusesTheProjectsObjectStore(t *testing.T) {
	gitOr(t, "git is required for the shadow store")
	project := newProject(t)
	store := t.TempDir()

	if _, err := OpenShadow(context.Background(), store, project); err != nil {
		t.Fatalf("OpenShadow: %v", err)
	}
	alternates := filepath.Join(store, "objects", "info", "alternates")
	content, err := os.ReadFile(alternates)
	if err != nil {
		t.Fatalf("reading alternates: %v", err)
	}
	want := filepath.Join(project, ".git", "objects")
	if strings.TrimSpace(string(content)) != want {
		t.Errorf("alternates = %q, want %q", strings.TrimSpace(string(content)), want)
	}
}

func TestShadowRefusesADirectoryThatIsNotARepository(t *testing.T) {
	gitOr(t, "git is required for the shadow store")
	if _, err := OpenShadow(context.Background(), t.TempDir(), t.TempDir()); err == nil {
		t.Fatal("OpenShadow accepted a directory with no .git, so nothing would fall back to copying")
	}
}

func TestStoreChoosesTheShadowStoreInARepository(t *testing.T) {
	gitOr(t, "git is required for the shadow store")
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.UseShadow(context.Background(), newProject(t))
	if store.Strategy() != StrategyShadow {
		t.Errorf("strategy = %q, want %q", store.Strategy(), StrategyShadow)
	}
	if store.Notice() != "" {
		t.Errorf("a working shadow store said something: %q", store.Notice())
	}
}

// Not a repository, no git, a store that will not open: all one answer to the
// caller, because the alternative to falling back is failing the turn.
func TestStoreFallsBackToCopyingOutsideARepository(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.UseShadow(context.Background(), t.TempDir()) // no .git
	if store.Strategy() != StrategyCopy {
		t.Errorf("strategy = %q, want %q", store.Strategy(), StrategyCopy)
	}
	if store.Notice() == "" {
		t.Error("kolk silently gave up on snapshotting what bash does; it must say so once")
	}
}

// A snapshot that fails mid-session must not fail the turn, and must not be
// retried every turn for the rest of the session.
func TestAFailingShadowStoreDropsToCopyingForTheRestOfTheSession(t *testing.T) {
	gitOr(t, "git is required for the shadow store")
	project := newProject(t)
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.UseShadow(context.Background(), project)
	if store.Strategy() != StrategyShadow {
		t.Fatalf("setup did not select the shadow store: %q", store.Strategy())
	}

	// The store goes away underneath us — a cleaner, a full disk, a bug.
	if err := os.RemoveAll(store.shadow.Dir()); err != nil {
		t.Fatal(err)
	}
	store.BeginTurn(context.Background())

	if store.Strategy() != StrategyCopy {
		t.Errorf("a broken shadow store was kept: %q", store.Strategy())
	}
	if store.Notice() == "" {
		t.Error("the fallback happened silently")
	}
	first := store.Notice()
	store.BeginTurn(context.Background())
	if store.Notice() != first {
		t.Errorf("the notice changed on a later turn: %q then %q", first, store.Notice())
	}
}

// Until L32.3 teaches rewind to read the shadow store, the copy store keeps
// recording. A snapshot layer that silently disabled /undo would be a
// regression wearing a feature's name.
func TestRecordKeepsWorkingWhileTheShadowStoreIsActive(t *testing.T) {
	gitOr(t, "git is required for the shadow store")
	project := newProject(t)
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.UseShadow(context.Background(), project)
	store.BeginTurn(context.Background())

	target := filepath.Join(project, "kept.txt")
	if err := store.Record("edit_file", target); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := os.WriteFile(target, []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	restored, err := store.RewindLastTurn()
	if err != nil {
		t.Fatalf("RewindLastTurn: %v", err)
	}
	if len(restored) != 1 {
		t.Fatalf("restored %v, want one path", restored)
	}
	if content, _ := os.ReadFile(target); string(content) != "one\n" {
		t.Errorf("the file was not put back: %q", content)
	}
}
