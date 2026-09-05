package checkpoint

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// A file the user keeps at 0600 -- an .env, a private key, a token file -- must
// come back at 0600. A rewind that restores the bytes but widens the mode has
// quietly made a secret world-readable, which is a worse state than the edit
// it undid.
func TestRewindPreservesARestrictiveModeInTheCopyStore(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes")
	}
	work, store := t.TempDir(), t.TempDir()
	p := filepath.Join(work, ".env")
	if err := os.WriteFile(p, []byte("TOKEN=v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(store)
	if err != nil {
		t.Fatal(err)
	}
	s.BeginTurn(context.Background())
	if err := s.Record("edit_file", p); err != nil {
		t.Fatal(err)
	}
	// The edit, then a `rm` by a later bash command: the file is gone when
	// /undo runs, so the restore has to create it -- and creating it is where
	// a mode gets invented. (An in-place rewrite keeps the inode and its mode;
	// that case never had the bug, and 1c.2's atomic replace would introduce
	// it, which is why the mode is recorded rather than trusted to the inode.)
	if err := os.WriteFile(p, []byte("TOKEN=v2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RewindLastTurn(context.Background()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode after rewind = %o, want 600", got)
	}
	if b, _ := os.ReadFile(p); string(b) != "TOKEN=v1\n" {
		t.Fatalf("content after rewind = %q", b)
	}
}

// The shadow store restores through git, and git recreates a changed file at
// the index mode filtered by umask -- 0644 for anything not executable. The
// user's 0600 must win over git's idea of a mode.
func TestRewindPreservesARestrictiveModeInTheShadowStore(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes")
	}
	gitOr(t, "git is required for the shadow store")
	project := newProject(t) // kept.txt is committed at 0600
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.UseShadow(context.Background(), project)
	if store.Strategy() != StrategyShadow {
		t.Fatalf("setup did not select the shadow store: %q", store.Strategy())
	}
	store.BeginTurn(context.Background())
	kept := filepath.Join(project, "kept.txt")
	if err := os.WriteFile(kept, []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RewindLastTurn(context.Background()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(kept)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode after shadow rewind = %o, want 600", got)
	}
	if b, _ := os.ReadFile(kept); string(b) != "one\n" {
		t.Fatalf("content after shadow rewind = %q", b)
	}
}
