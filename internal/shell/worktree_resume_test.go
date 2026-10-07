package shell

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWorktreeResumeVerifiesOwnerAndDirectory(t *testing.T) {
	root, _ := testRepo(t)
	other, _ := testRepo(t)
	store := t.TempDir()
	w := NewWorktreeIsolator(store)
	ctx := context.Background()
	dir, err := w.Isolate(ctx, root, "task")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Release(ctx, root, dir)
	if err := w.Resume(ctx, root, "task", dir); err != nil {
		t.Fatalf("owned tree refused: %v", err)
	}
	for _, test := range []struct{ root, name, dir string }{
		{other, "task", dir}, {root, "other", dir}, {root, "../task", dir}, {root, "task", root},
	} {
		if err := w.Resume(ctx, test.root, test.name, test.dir); err == nil {
			t.Fatalf("accepted foreign tree: %+v", test)
		}
	}
	alias := filepath.Join(store, "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	if err := w.Resume(ctx, root, "alias", alias); err == nil {
		t.Fatal("accepted a replacement symlink")
	}
	w.Release(ctx, root, dir)
	if err := w.Resume(ctx, root, "task", dir); err == nil {
		t.Fatal("accepted removed tree")
	}
}
