package checkpoint

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A rewind writes bytes to a path it recorded earlier. Between the two, a bash
// command -- the model's, or a background one it left running -- can replace
// that path with a symlink to somewhere else. Writing "the file back" would
// then write the backup's bytes into a file outside the project. The restore
// has to notice, refuse, and leave both the target and the backup alone.
func TestRewindRefusesToWriteThroughASymlinkPlantedAtThePath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics")
	}
	work, outside := t.TempDir(), t.TempDir()
	victim := filepath.Join(outside, "authorized_keys")
	if err := os.WriteFile(victim, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.UseShadow(context.Background(), work) // no .git: the copy store, confined to work
	p := filepath.Join(work, "a.txt")
	if err := os.WriteFile(p, []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.BeginTurn(context.Background())
	if err := s.Record("edit_file", p); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The swap.
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, p); err != nil {
		t.Fatal(err)
	}

	_, err = s.RewindLastTurn(context.Background())
	if got, _ := os.ReadFile(victim); string(got) != "keep\n" {
		t.Fatalf("the rewind wrote through the symlink: %s = %q", victim, got)
	}
	if err == nil {
		t.Fatal("the rewind reported success while the path was a symlink out of the project")
	}
	if !strings.Contains(err.Error(), "a.txt") {
		t.Errorf("the refusal does not name the path: %v", err)
	}
}

// The same escape one level up: the file's directory is swapped for a symlink,
// so the recorded path still "exists" but lives somewhere else now.
func TestRewindRefusesToWriteThroughASymlinkedParentDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics")
	}
	work, outside := t.TempDir(), t.TempDir()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.UseShadow(context.Background(), work)
	sub := filepath.Join(work, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(sub, "a.txt")
	if err := os.WriteFile(p, []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.BeginTurn(context.Background())
	if err := s.Record("edit_file", p); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(sub); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, sub); err != nil {
		t.Fatal(err)
	}

	_, err = s.RewindLastTurn(context.Background())
	if _, statErr := os.Lstat(filepath.Join(outside, "a.txt")); statErr == nil {
		t.Fatalf("the rewind created %s outside the project through the symlinked directory", filepath.Join(outside, "a.txt"))
	}
	if err == nil {
		t.Fatal("the rewind reported success while the directory was a symlink out of the project")
	}
}
