package checkpoint

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Taking back task one must not take back task two. A file both tasks changed
// carries task two's work on top of task one's; restoring it to before task one
// erases both, which is what /undo of the whole turn is for. The rewind keeps
// such a path, says so, and restores the rest.
func TestRewindTaskKeepsAPathALaterTaskAlsoChanged(t *testing.T) {
	ctx := context.Background()
	store, project := storeOnProject(t)

	first := store.BeginTask(ctx, "first")
	write(t, project, "only-first.txt", "first\n")
	write(t, project, "shared.txt", "by first\n")
	store.EndTask(ctx, first)

	second := store.BeginTask(ctx, "second")
	write(t, project, "shared.txt", "by first, then second\n")
	store.EndTask(ctx, second)

	result, err := store.RewindTask(ctx, 1)
	if err != nil {
		t.Fatalf("RewindTask: %v", err)
	}
	if got := read(t, project, "shared.txt"); got != "by first, then second\n" {
		t.Errorf("shared.txt = %q; the later task's work was erased", got)
	}
	if exists(project, "only-first.txt") {
		t.Error("only-first.txt survived; the earlier task's own file must go")
	}
	if len(result.Restored) != 1 || len(result.Kept) != 1 {
		t.Errorf("result = %+v, want one restored and one kept", result)
	}
}

// A snapshot that has been restored is spent: listing shows it, and restoring
// it again is refused rather than silently re-applied to whatever is there now.
func TestARewoundTaskSnapshotIsConsumedAndStaysConsumedOnReopen(t *testing.T) {
	ctx := context.Background()
	store, project := storeOnProject(t)
	task := store.BeginTask(ctx, "the task")
	write(t, project, "made.txt", "made\n")
	store.EndTask(ctx, task)

	if _, err := store.RewindTask(ctx, 1); err != nil {
		t.Fatalf("first rewind: %v", err)
	}
	if !store.TaskSnapshots()[0].Consumed {
		t.Fatal("the restored snapshot is not marked consumed")
	}
	if _, err := store.RewindTask(ctx, 1); err == nil {
		t.Fatal("the same task was taken back twice")
	}
	reopened, err := Open(store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if snaps := reopened.TaskSnapshots(); len(snaps) != 1 || !snaps[0].Consumed {
		t.Fatalf("consumption did not survive a reopen: %+v", snaps)
	}
}

func exists(project, name string) bool {
	_, err := os.Stat(filepath.Join(project, name))
	return err == nil
}
