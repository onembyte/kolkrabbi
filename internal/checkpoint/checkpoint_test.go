package checkpoint

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRewindRestoresEditedFile(t *testing.T) {
	work, store := t.TempDir(), t.TempDir()
	p := filepath.Join(work, "a.txt")
	os.WriteFile(p, []byte("v1"), 0o644)

	s, err := Open(store)
	if err != nil {
		t.Fatal(err)
	}
	s.BeginTurn(context.Background())
	if err := s.Record("edit_file", p); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(p, []byte("v2"), 0o644) // the "edit"

	restored, err := s.RewindLastTurn(context.Background())
	if err != nil {
		t.Fatalf("rewind: %v", err)
	}
	if len(restored) != 1 {
		t.Fatalf("restored %d paths, want 1", len(restored))
	}
	b, _ := os.ReadFile(p)
	if string(b) != "v1" {
		t.Errorf("file = %q, want v1", string(b))
	}
	// second rewind: nothing left
	restored, err = s.RewindLastTurn(context.Background())
	if err != nil || restored != nil {
		t.Errorf("second rewind = (%v,%v), want (nil,nil)", restored, err)
	}
}

func TestRewindDeletesCreatedFile(t *testing.T) {
	work, store := t.TempDir(), t.TempDir()
	p := filepath.Join(work, "new.txt")

	s, _ := Open(store)
	s.BeginTurn(context.Background())
	if err := s.Record("write_file", p); err != nil { // file does not exist yet
		t.Fatal(err)
	}
	os.WriteFile(p, []byte("hello"), 0o644) // the "write"

	if _, err := s.RewindLastTurn(context.Background()); err != nil {
		t.Fatalf("rewind: %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("created file still exists after rewind")
	}
}

func TestRewindIsPerTurnAndOrdered(t *testing.T) {
	work, store := t.TempDir(), t.TempDir()
	p := filepath.Join(work, "a.txt")
	os.WriteFile(p, []byte("turn0"), 0o644)

	s, _ := Open(store)

	s.BeginTurn(context.Background()) // turn 1
	s.Record("edit_file", p)
	os.WriteFile(p, []byte("turn1"), 0o644)

	s.BeginTurn(context.Background()) // turn 2: two changes to the same file
	s.Record("edit_file", p)
	os.WriteFile(p, []byte("turn2a"), 0o644)
	s.Record("edit_file", p)
	os.WriteFile(p, []byte("turn2b"), 0o644)

	// rewinding must undo only turn 2, restoring the pre-turn-2 state
	if _, err := s.RewindLastTurn(context.Background()); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "turn1" {
		t.Errorf("after rewinding turn 2, file = %q, want turn1", string(b))
	}

	// and rewinding again undoes turn 1
	if _, err := s.RewindLastTurn(context.Background()); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(p)
	if string(b) != "turn0" {
		t.Errorf("after rewinding turn 1, file = %q, want turn0", string(b))
	}
}

func TestStorePersistsAcrossReopen(t *testing.T) {
	work, store := t.TempDir(), t.TempDir()
	p := filepath.Join(work, "a.txt")
	os.WriteFile(p, []byte("v1"), 0o644)

	s1, _ := Open(store)
	s1.BeginTurn(context.Background())
	s1.Record("edit_file", p)
	os.WriteFile(p, []byte("v2"), 0o644)

	// reopen (simulates kolk restart + --resume) and rewind from the new store
	s2, err := Open(store)
	if err != nil {
		t.Fatal(err)
	}
	if len(s2.Changes()) != 1 {
		t.Fatalf("reopened store has %d entries, want 1", len(s2.Changes()))
	}
	if _, err := s2.RewindLastTurn(context.Background()); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "v1" {
		t.Errorf("file = %q, want v1", string(b))
	}
}
