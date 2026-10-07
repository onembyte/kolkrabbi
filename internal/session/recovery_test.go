package session

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

func TestRecoverySnapshotIsCompressedAndLoadsWithoutAJSONMirror(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "gateway/model")
	s.AppendMessage(provider.Message{Role: "user", Content: strings.Repeat("the entire conversation ", 100)})
	s.SetRunState(&continuity.Run{Version: 1, ID: "turn-current", Input: "continue", Phase: "tasks",
		Tasks: []continuity.Task{{Title: "finished child", Model: "gateway/model", Status: "done", Result: "already done"}}})
	if err := s.SaveRecovery("pause"); err != nil {
		t.Fatal(err)
	}
	compressed, err := os.ReadFile(filepath.Join(dir, s.ID+".resume.json.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if len(compressed) >= len(strings.Repeat("the entire conversation ", 100)) {
		t.Fatalf("recovery was not compressed: %d bytes", len(compressed))
	}
	if err := os.Remove(filepath.Join(dir, s.ID+".json")); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.RunState(); got == nil || got.Tasks[0].Result != "already done" {
		t.Fatalf("lost settled child: %+v", got)
	}
	listed, err := List(dir)
	if err != nil || len(listed) != 1 || listed[0].ID != s.ID {
		t.Fatalf("sidecar-only session was not listed: %+v, %v", listed, err)
	}
}

func TestRecoveryLoadsNewestRevisionAndRetainsArchivedConversations(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "gateway/model")
	archive, err := s.ArchiveMessages([]provider.Message{{Role: "assistant", Content: "full child history"}})
	if err != nil {
		t.Fatal(err)
	}
	s.SetRunState(&continuity.Run{Version: 1, ID: "previous", Phase: "done"})
	s.SetRunState(&continuity.Run{Version: 1, ID: "current", Phase: "tasks",
		Tasks: []continuity.Task{{Title: "child", Model: "gateway/model", Archives: []string{archive}}}})
	s.AppendMessage(provider.Message{Role: "user", Content: "snapshot conversation"})
	if err := s.SaveRecovery("limit"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{s.compactionDir(), s.executionDir()} {
		if err := os.RemoveAll(path); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := Load(dir, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	history, err := loaded.CompactionHistory()
	if err != nil || len(history) != 1 || history[0].Messages[0].Content != "full child history" {
		t.Fatalf("archive recovery: %+v, %v", history, err)
	}
	runs, err := loaded.ExecutionHistory()
	if err != nil || len(runs) != 2 {
		t.Fatalf("execution history recovery: %+v, %v", runs, err)
	}
	loaded.AppendMessage(provider.Message{Role: "assistant", Content: "newer ordinary save"})
	if err := loaded.Save(); err != nil {
		t.Fatal(err)
	}
	newest, err := Load(dir, s.ID)
	if err != nil || len(newest.GetMessages()) != 2 {
		t.Fatalf("newer JSON lost: %+v, %v", newest, err)
	}
	if err := os.Remove(archive); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir, s.ID); err != nil {
		t.Fatalf("newer JSON could not recover its immutable archive: %v", err)
	}
}

func TestRecoveryRefusesCorruptionAndArchiveEscape(t *testing.T) {
	for _, defect := range []string{"version", "session checksum", "archive checksum", "archive path", "archive content", "trailing member", "parent symlink"} {
		t.Run(defect, func(t *testing.T) {
			dir := t.TempDir()
			s := New(dir, "model")
			archive, err := s.ArchiveMessages([]provider.Message{{Role: "assistant", Content: "kept"}})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.SaveRecovery("error"); err != nil {
				t.Fatal(err)
			}
			e := readRecoveryEnvelopeForTest(t, recoveryPath(dir, s.ID))
			switch defect {
			case "version":
				e.Version++
			case "session checksum":
				e.SessionSHA256 = "bad"
			case "archive checksum":
				e.Archives[0].SHA256 = "bad"
			case "archive path":
				e.Archives[0].Name = "../escaped.json"
			case "archive content":
				e.Archives[0].Data = []byte("invalid archive")
				e.Archives[0].SHA256 = digest(e.Archives[0].Data)
				if err := os.Remove(archive); err != nil {
					t.Fatal(err)
				}
			case "parent symlink":
				if err := os.RemoveAll(s.compactionDir()); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(t.TempDir(), s.compactionDir()); err != nil {
					t.Skip(err)
				}
			}
			data := encodeRecoveryForTest(t, e)
			if defect == "trailing member" {
				data = append(data, data...)
			}
			if err := os.WriteFile(recoveryPath(dir, s.ID), data, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(dir, s.ID); err == nil {
				t.Fatal("accepted unsafe recovery point")
			}
		})
	}
}

func TestRecoverySaveRejectsAMissingReferencedArchive(t *testing.T) {
	s := New(t.TempDir(), "model")
	s.SetRunState(&continuity.Run{Version: 1, ID: "current", Phase: "tasks",
		Tasks: []continuity.Task{{Title: "child", Archives: []string{filepath.Join(s.compactionDir(), "missing.json")}}}})
	if err := s.SaveRecovery("pause"); err == nil {
		t.Fatal("claimed complete snapshot with missing child history")
	}
}

func TestRecoverySnapshotMustContainEveryReferencedArchive(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "model")
	path, err := s.ArchiveMessages([]provider.Message{{Role: "user", Content: "history"}})
	if err != nil {
		t.Fatal(err)
	}
	s.SetRunState(&continuity.Run{ID: "run", Main: continuity.Task{Archives: []string{path}}})
	if err := s.SaveRecovery("pause"); err != nil {
		t.Fatal(err)
	}
	e := readRecoveryEnvelopeForTest(t, recoveryPath(dir, s.ID))
	e.Archives = nil
	if err := os.WriteFile(recoveryPath(dir, s.ID), encodeRecoveryForTest(t, e), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir, s.ID); err == nil {
		t.Fatal("incomplete snapshot borrowed required history from disk")
	}
}

func TestRecoveryListRejectsSidecarDirectory(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "model")
	if err := os.Mkdir(recoveryPath(dir, s.ID), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := List(dir); err == nil {
		t.Fatal("invalid recovery point disappeared from listing")
	}
}

func TestRecoverySaveRejectsMissingHistoryInAnArchivedRun(t *testing.T) {
	s := New(t.TempDir(), "model")
	s.SetRunState(&continuity.Run{Version: 1, ID: "previous", Phase: "done",
		Tasks: []continuity.Task{{Title: "child", Archives: []string{filepath.Join(s.compactionDir(), "missing.json")}}}})
	s.SetRunState(&continuity.Run{Version: 1, ID: "current", Phase: "tasks"})
	if err := s.SaveRecovery("pause"); err == nil {
		t.Fatal("claimed complete snapshot with missing archived child history")
	}
}

func TestRecoveryNeverWritesThroughAnExecutionArchiveSymlink(t *testing.T) {
	s := New(t.TempDir(), "model")
	outside := t.TempDir()
	if err := os.Symlink(outside, s.executionDir()); err != nil {
		t.Skip(err)
	}
	s.SetRunState(&continuity.Run{ID: "older", Phase: "done"})
	s.SetRunState(&continuity.Run{ID: "current", Phase: "tasks"})
	if err := s.SaveRecovery("pause"); err == nil {
		t.Fatal("accepted linked archive store")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("recovery wrote outside its store: %v, %v", entries, err)
	}
}

func TestRecoveryReportsHeaderFailureAfterWritingTheRecoveryPoint(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "model")
	if err := os.Mkdir(metaPath(dir, s.ID), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRecovery("pause"); err == nil || !strings.Contains(err.Error(), "header") {
		t.Fatalf("header failure was not reported: %v", err)
	}
	if _, err := Load(dir, s.ID); err != nil {
		t.Fatalf("durable recovery point was not loadable: %v", err)
	}
}

func TestRecoveryRevisionPrecedenceAndLifecycle(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "old-model")
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	oldJSON, err := os.ReadFile(s.path())
	if err != nil {
		t.Fatal(err)
	}
	s.SetModelName("new-model")
	if err := s.SaveRecovery("pause"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.path(), oldJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir, s.ID)
	if err != nil || loaded.ModelName() != "new-model" {
		t.Fatalf("stale mirror won: %+v, %v", loaded, err)
	}
	if _, err := RepairMeta(dir, s.ID); err != nil {
		t.Fatal(err)
	}
	listed, err := Latest(dir)
	if err != nil || listed.ModelName() != "new-model" {
		t.Fatalf("latest did not select recovery: %+v, %v", listed, err)
	}
	if err := os.Remove(s.path()); err != nil {
		t.Fatal(err)
	}
	if err := Delete(dir, s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(recoveryPath(dir, s.ID)); !os.IsNotExist(err) {
		t.Fatalf("deleted session left recovery: %v", err)
	}
	if listed, err := List(dir); err != nil || len(listed) != 0 {
		t.Fatalf("deleted recovery still listed: %+v, %v", listed, err)
	}
}

func TestRecoveryMirrorRequiresCompleteHistoryWhenSidecarIsLost(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "model")
	archive, err := s.ArchiveMessages([]provider.Message{{Role: "user", Content: "history"}})
	if err != nil {
		t.Fatal(err)
	}
	s.SetRunState(&continuity.Run{ID: "run", Main: continuity.Task{Archives: []string{archive}}})
	if err := s.SaveRecovery("pause"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(recoveryPath(dir, s.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir, s.ID); err != nil {
		t.Fatalf("valid mirror did not recover: %v", err)
	}
	if err := os.Remove(archive); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir, s.ID); err == nil {
		t.Fatal("missing sidecar disabled recovery validation")
	}
}

func TestRecoveryFailedReplacementPreservesThePreviousBoundary(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "model")
	s.AppendMessage(provider.Message{Role: "user", Content: "saved boundary"})
	if err := s.SaveRecovery("pause"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(recoveryPath(dir, s.ID))
	if err != nil {
		t.Fatal(err)
	}
	s.AppendMessage(provider.Message{Role: "assistant", Content: "not durable"})
	s.SetRunState(&continuity.Run{ID: "run", Main: continuity.Task{Archives: []string{filepath.Join(s.compactionDir(), "absent.json")}}})
	if err := s.SaveRecovery("error"); err == nil {
		t.Fatal("broken replacement claimed success")
	}
	after, err := os.ReadFile(recoveryPath(dir, s.ID))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed replacement damaged previous recovery point")
	}
	loaded, err := Load(dir, s.ID)
	if err != nil || len(loaded.GetMessages()) != 1 {
		t.Fatalf("previous boundary was lost: %+v, %v", loaded, err)
	}
	info, err := os.Stat(recoveryPath(dir, s.ID))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("recovery permissions: %v, %v", info, err)
	}
}

func TestRecoveryRefusesRevisionOverflow(t *testing.T) {
	for _, recovery := range []bool{false, true} {
		s := New(t.TempDir(), "model")
		s.Revision = ^uint64(0)
		var err error
		if recovery {
			err = s.SaveRecovery("pause")
		} else {
			err = s.Save()
		}
		if err == nil {
			t.Errorf("recovery=%v: revision wrapped and could make an older record win", recovery)
		}
	}
}

func TestRecoverySnapshotSerializesConcurrentStateChanges(t *testing.T) {
	s := New(t.TempDir(), "model")
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 20 {
			s.SetTitle("title")
			s.SetEffort("high")
			s.SetMode("agent")
			s.SetProviderStateName("confirmed-handle")
		}
	}()
	for range 20 {
		if err := s.SaveRecovery("pause"); err != nil {
			t.Error(err)
		}
	}
	wg.Wait()
}

func TestRecoveryEffortUpdateSharesTheSnapshotLock(t *testing.T) {
	s := New(t.TempDir(), "model")
	s.messagesMu.Lock()
	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		close(started)
		s.SetEffort("high")
		close(done)
	}()
	<-started
	select {
	case <-done:
		s.messagesMu.Unlock()
		t.Fatal("effort changed while a recovery snapshot held the state lock")
	case <-time.After(20 * time.Millisecond):
	}
	s.messagesMu.Unlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("effort update stayed blocked after the snapshot lock was released")
	}
}

func readRecoveryEnvelopeForTest(t *testing.T, path string) recoveryEnvelope {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	plain, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	var e recoveryEnvelope
	if err := json.Unmarshal(plain, &e); err != nil {
		t.Fatal(err)
	}
	return e
}

func encodeRecoveryForTest(t *testing.T, e recoveryEnvelope) []byte {
	t.Helper()
	plain, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	z := gzip.NewWriter(&data)
	if _, err := z.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestRecoveryLoadFailsClosedOnEitherCorruptCandidate(t *testing.T) {
	for _, corrupt := range []string{"resume.json.gz", "json"} {
		t.Run(corrupt, func(t *testing.T) {
			dir := t.TempDir()
			s := New(dir, "gateway/model")
			if err := s.SaveRecovery("error"); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, s.ID+"."+corrupt), []byte("broken"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(dir, s.ID); err == nil {
				t.Fatal("loaded an older candidate after an unreadable recovery file")
			}
			if _, err := List(dir); err == nil {
				t.Fatal("listing hid an unreadable recovery file")
			}
		})
	}
}
