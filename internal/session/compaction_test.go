package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

func TestConcurrentCompactionArchivesSurviveRestartOutsideHotSession(t *testing.T) {
	s := New(t.TempDir(), "model")
	var wg sync.WaitGroup
	for i := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			messages := []provider.Message{{Role: "user", Content: fmt.Sprintf("child %d: %s", i, strings.Repeat("x", 8192))}}
			first, err := s.ArchiveMessages(messages)
			if err != nil {
				t.Error(err)
				return
			}
			second, err := s.ArchiveMessages(messages)
			if err != nil || second != first {
				t.Errorf("identical history duplicated: %s %s %v", first, second, err)
			}
		}()
	}
	wg.Wait()
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(s.dir, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	archives, err := loaded.CompactionHistory()
	if err != nil || len(archives) != 12 {
		t.Fatalf("archives after restart = %d: %v", len(archives), err)
	}
	for _, archive := range archives {
		if len(archive.Messages) != 1 || len(archive.Messages[0].Content) < 8192 {
			t.Fatal("archive lost raw content")
		}
	}
	info, err := os.Stat(s.path())
	if err != nil || info.Size() > 4096 {
		t.Fatalf("history inflated routine session save: %v %v", info, err)
	}
	if err := Delete(s.dir, s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.compactionDir()); !os.IsNotExist(err) {
		t.Fatal("session deletion left retained history behind")
	}
}

func TestCompactionHistoryIncludesLegacyAndRejectsCorruption(t *testing.T) {
	s := New(t.TempDir(), "model")
	legacy := filepath.Join(s.dir, s.ID+".pre-compact-1.json")
	if err := os.WriteFile(legacy, []byte(`[{"role":"user","content":"legacy goal"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	path, err := s.ArchiveMessages([]provider.Message{{Role: "user", Content: "current goal"}})
	if err != nil {
		t.Fatal(err)
	}
	archives, err := s.CompactionHistory()
	if err != nil || len(archives) != 2 {
		t.Fatalf("legacy history missing: %v %v", archives, err)
	}
	if err := os.WriteFile(path, []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompactionHistory(); err == nil {
		t.Fatal("corrupted archive silently accepted")
	}
}

func TestCompactionArchiveRejectsReplacedStore(t *testing.T) {
	s := New(t.TempDir(), "model")
	if err := os.Symlink(t.TempDir(), s.compactionDir()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ArchiveMessages([]provider.Message{{Role: "user", Content: "goal"}}); err == nil {
		t.Fatal("archive wrote through a replaced store")
	}
	if _, err := s.CompactionHistory(); err == nil {
		t.Fatal("history read through a replaced store")
	}
}
