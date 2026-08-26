package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewSessionRecordsWhereItWasStarted(t *testing.T) {
	dir := t.TempDir()
	work := t.TempDir()
	t.Chdir(work)

	s := New(dir, "vendor/model")
	if s.CWD == "" {
		t.Fatal("a session must record the project it belongs to")
	}
	resolved, err := filepath.EvalSymlinks(work)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := filepath.EvalSymlinks(s.CWD); got != resolved {
		t.Fatalf("cwd = %q, want %q", s.CWD, resolved)
	}
}

func TestLatestForDirPrefersThisProject(t *testing.T) {
	dir := t.TempDir()
	here, elsewhere := t.TempDir(), t.TempDir()

	older := New(dir, "vendor/model")
	older.CWD = here
	older.Title = "in this project"
	if err := older.Save(); err != nil {
		t.Fatal(err)
	}
	newer := New(dir, "vendor/model")
	newer.CWD = elsewhere
	newer.Title = "somewhere else"
	if err := newer.Save(); err != nil {
		t.Fatal(err)
	}

	// The newest session overall belongs to another project. Resuming here must
	// pick up this project's work, which is what "resume" means to a person
	// standing in a directory.
	got, err := LatestForDir(dir, here)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Title != "in this project" {
		t.Fatalf("resumed %+v, want this project's session", got)
	}
}

func TestLatestForDirFallsBackToTheNewestOverall(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()

	s := New(dir, "vendor/model")
	s.CWD = other
	s.Title = "somewhere else"
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	got, err := LatestForDir(dir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Title != "somewhere else" {
		t.Fatalf("resumed %+v, want the newest overall when this project has none", got)
	}
}

func TestLatestForDirIgnoresSessionsWrittenBeforeThisFieldExisted(t *testing.T) {
	// Sessions saved by an older Kolkrabbi have no cwd. They must stay
	// reachable by the global fallback rather than matching every directory.
	dir := t.TempDir()
	legacy := New(dir, "vendor/model")
	legacy.CWD = ""
	legacy.Title = "before cwd existed"
	if err := legacy.Save(); err != nil {
		t.Fatal(err)
	}

	got, err := LatestForDir(dir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Title != "before cwd existed" {
		t.Fatalf("resumed %+v, want the legacy session through the fallback", got)
	}
}

func TestLatestForDirWithNoSessionsIsNotAnError(t *testing.T) {
	got, err := LatestForDir(t.TempDir(), t.TempDir())
	if err != nil || got != nil {
		t.Fatalf("got %+v, err %v", got, err)
	}
}

func TestDeleteRemovesCompactionArchivesToo(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "vendor/model")
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	// A compaction archive holds the conversation that was replaced, so a
	// deleted session that leaves one behind is still readable on disk.
	archive := filepath.Join(dir, s.ID+".pre-compact-1.json")
	if err := os.WriteFile(archive, []byte(`[{"role":"user","content":"secret"}]`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := Delete(dir, s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(archive); err == nil {
		t.Fatal("deleting the session left its replaced conversation on disk")
	}
}

func TestDeleteLeavesOtherSessionsArchivesAlone(t *testing.T) {
	dir := t.TempDir()
	mine := New(dir, "vendor/model")
	if err := mine.Save(); err != nil {
		t.Fatal(err)
	}
	other := New(dir, "vendor/model")
	if err := other.Save(); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dir, other.ID+".pre-compact-1.json")
	if err := os.WriteFile(keep, []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := Delete(dir, mine.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("another session's archive was removed: %v", err)
	}
}
