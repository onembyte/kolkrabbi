package checkpoint

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func storeWith(t *testing.T) (*Store, string) {
	t.Helper()
	work := t.TempDir()
	store, err := Open(filepath.Join(t.TempDir(), "ckpt"))
	if err != nil {
		t.Fatal(err)
	}
	return store, work
}

func TestOriginalIsTheStateBeforeTheSessionTouchedIt(t *testing.T) {
	store, work := storeWith(t)
	path := filepath.Join(work, "a.go")
	if err := os.WriteFile(path, []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Two turns editing the same file. The session diff has to be against what
	// was there before any of it, not before the most recent edit.
	store.BeginTurn(context.Background())
	if err := store.Record("edit_file", path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store.BeginTurn(context.Background())
	if err := store.Record("edit_file", path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("third\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	content, existed, err := store.Original(path)
	if err != nil {
		t.Fatal(err)
	}
	if !existed {
		t.Fatal("reported the file as new")
	}
	if string(content) != "first\n" {
		t.Fatalf("original = %q, want the state before the first edit", content)
	}
}

func TestAFileTheSessionCreatedHasNoOriginal(t *testing.T) {
	store, work := storeWith(t)
	path := filepath.Join(work, "new.go")

	store.BeginTurn(context.Background())
	if err := store.Record("write_file", path); err != nil {
		t.Fatal(err)
	}

	content, existed, err := store.Original(path)
	if err != nil {
		t.Fatal(err)
	}
	if existed {
		t.Fatal("a created file reported an original")
	}
	if len(content) != 0 {
		t.Fatalf("content = %q, want nothing", content)
	}
}

func TestAnUntouchedFileIsNotKnown(t *testing.T) {
	store, work := storeWith(t)

	if _, _, err := store.Original(filepath.Join(work, "never.go")); err == nil {
		t.Fatal("a file the session never touched reported an original")
	}
}

func TestChangedPathsAreListedOnceInOrder(t *testing.T) {
	store, work := storeWith(t)
	for _, name := range []string{"b.go", "a.go", "b.go"} {
		path := filepath.Join(work, name)
		if err := os.WriteFile(path, []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		store.BeginTurn(context.Background())
		if err := store.Record("edit_file", path); err != nil {
			t.Fatal(err)
		}
	}

	got := store.ChangedPaths()

	// A file edited three times is one changed file, and the order someone
	// first touched it is the order they will look for it.
	if len(got) != 2 {
		t.Fatalf("got %v, want two paths", got)
	}
	if filepath.Base(got[0]) != "b.go" || filepath.Base(got[1]) != "a.go" {
		t.Fatalf("got %v, want first-touched order", got)
	}
}
