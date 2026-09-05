package checkpoint

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// write is a shorthand for the "a subagent edited this" step.
func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return ""
	}
	return string(b)
}

// storeOnProject opens a checkpoint store with a shadow attached to a real
// repository, or skips when this machine cannot do that.
func storeOnProject(t *testing.T) (*Store, string) {
	t.Helper()
	gitOr(t, "git is required for per-task snapshots")
	project := newProject(t)
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.UseShadow(context.Background(), project)
	if store.Strategy() != StrategyShadow {
		t.Skipf("no shadow store here: %s", store.Notice())
	}
	store.BeginTurn(context.Background())
	return store, project
}

func TestRewindTaskTakesBackOneSubagentAndLeavesTheRest(t *testing.T) {
	ctx := context.Background()
	store, project := storeOnProject(t)

	// Three writing subagents, run one after another as the scheduler does.
	first := store.BeginTask(ctx, "rename the port")
	write(t, project, "first.txt", "first\n")
	write(t, project, "shared.txt", "written by one\n")
	store.EndTask(ctx, first)

	second := store.BeginTask(ctx, "make a mess")
	write(t, project, "mess.txt", "junk\n")
	write(t, project, "kept.txt", "clobbered\n")
	store.EndTask(ctx, second)

	third := store.BeginTask(ctx, "the good work after it")
	write(t, project, "third.txt", "third\n")
	store.EndTask(ctx, third)

	snapshots := store.TaskSnapshots()
	if len(snapshots) != 3 {
		t.Fatalf("recorded %d task snapshots, want 3", len(snapshots))
	}
	if snapshots[1].Title != "make a mess" {
		t.Fatalf("second snapshot titled %q, want the task's own title", snapshots[1].Title)
	}

	result, err := store.RewindTask(ctx, 2)
	if err != nil {
		t.Fatalf("RewindTask: %v", err)
	}
	restored := result.Restored

	// The messy task is gone: its new file removed, its clobbering reverted.
	if _, err := os.Stat(filepath.Join(project, "mess.txt")); !os.IsNotExist(err) {
		t.Error("mess.txt survived the rewind; a file the task created must go with it")
	}
	if got := read(t, project, "kept.txt"); got != "one\n" {
		t.Errorf("kept.txt = %q, want the content from before the task ran", got)
	}
	// And nothing else moved. This is the whole point of a per-task rewind:
	// restoring the tree to that snapshot would have taken the third task's
	// work with it, which is not rewinding one task on its own.
	if got := read(t, project, "third.txt"); got != "third\n" {
		t.Errorf("third.txt = %q, want the later task's work untouched", got)
	}
	if got := read(t, project, "first.txt"); got != "first\n" {
		t.Errorf("first.txt = %q, want the earlier task's work untouched", got)
	}
	if len(restored) != 2 {
		t.Errorf("restored %v, want exactly the two paths that task touched", restored)
	}
}

func TestRewindTaskRefusesAnIndexNobodyRan(t *testing.T) {
	ctx := context.Background()
	store, project := storeOnProject(t)
	only := store.BeginTask(ctx, "the only task")
	write(t, project, "one.txt", "x\n")
	store.EndTask(ctx, only)

	for _, n := range []int{0, 2, -1} {
		if _, err := store.RewindTask(ctx, n); err == nil {
			t.Fatalf("RewindTask(%d) succeeded, want a refusal naming what there is", n)
		}
	}
}

func TestBeginTaskIsSilentWithoutAShadowStore(t *testing.T) {
	// The copy store snapshots single files before a write, so it cannot
	// answer "what did this whole subagent change". Saying so once beats
	// pretending, and it must never fail a run.
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	handle := store.BeginTask(ctx, "anything")
	store.EndTask(ctx, handle)
	if got := len(store.TaskSnapshots()); got != 0 {
		t.Fatalf("recorded %d snapshots without a shadow store, want 0", got)
	}
	if _, err := store.RewindTask(ctx, 1); err == nil {
		t.Fatal("RewindTask succeeded without a shadow store, want a refusal")
	}
}
