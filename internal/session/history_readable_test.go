package session

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

// Adopted from the V43.5 item 5 re-check (J1): ExecutionHistory stays all or
// nothing for callers that must not lose a record silently (fork), and names
// the journal it could not read; ExecutionHistoryReadable keeps the rest.
func TestADamagedJournalIsRefusedStrictlyAndNamedReadably(t *testing.T) {
	s := New(t.TempDir(), "model")
	s.SetRunState(&continuity.Run{ID: "earlier", Phase: "done"})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	s.SetRunState(&continuity.Run{ID: "later", Phase: "done"})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(s.executionDir(), executionFile("earlier"))
	if err := os.WriteFile(journal, []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExecutionHistory(); err == nil || !strings.Contains(err.Error(), journal) {
		t.Fatalf("strict history = %v, want an error naming %s", err, journal)
	}
	runs, unreadable := s.ExecutionHistoryReadable()
	if len(unreadable) != 1 || len(runs) != 1 || runs[0].ID != "later" {
		t.Fatalf("readable history = %d runs, %v unreadable", len(runs), unreadable)
	}
}

// Adopted from the V43.5 item 6 verification (the k1last gap): the strict
// form's error is the first unreadable archive in listing order, the same one
// its readable twin reports first, not whichever came last.
func TestStrictHistoryReportsTheFirstUnreadableArchive(t *testing.T) {
	s := New(t.TempDir(), "model")
	var paths []string
	for _, content := range []string{"one", "two"} {
		path, err := s.ArchiveMessages([]provider.Message{{Role: "user", Content: content}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("damaged"), 0o600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	_, unreadable := s.CompactionHistoryReadable()
	_, err := s.CompactionHistory()
	if len(unreadable) != 2 || err == nil || err.Error() != unreadable[0].Error() || !strings.Contains(err.Error(), paths[0]) {
		t.Fatalf("strict = %v; readable reported %v; want the first, %s", err, unreadable, paths[0])
	}
}
