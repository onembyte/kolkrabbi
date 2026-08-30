package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/xid"
)

func TestSessionPathOperationsRejectTraversal(t *testing.T) {
	root := t.TempDir()
	sessions := filepath.Join(root, "sessions")
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(root, "victim.json")
	if err := os.WriteFile(victim, []byte(`{"id":"s_01J00000000000000000000000"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(sessions, "../victim"); err == nil {
		t.Fatal("Load accepted a path-traversing session id")
	}
	if err := Delete(sessions, "../victim"); err == nil {
		t.Fatal("Delete accepted a path-traversing session id")
	}
	if _, err := CompactionArchives(sessions, "../victim"); err == nil {
		t.Fatal("CompactionArchives accepted a path-traversing session id")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("path traversal removed the sibling victim: %v", err)
	}
}

func TestSaveRejectsAnInvalidSessionID(t *testing.T) {
	root := t.TempDir()
	victim := filepath.Join(root, "victim.json")
	if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	s := &Session{ID: "../victim", dir: filepath.Join(root, "sessions")}
	if err := s.Save(); err == nil {
		t.Fatal("Save accepted a path-traversing session id")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("invalid Save touched the sibling victim: %v", err)
	}
}

func TestLoadRejectsAFileWhoseDecodedIDDoesNotMatchItsFilename(t *testing.T) {
	dir := t.TempDir()
	filenameID := xid.New(xid.Session)
	decodedID := xid.New(xid.Session)
	body, err := json.Marshal(map[string]any{"id": decodedID, "messages": []any{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, filenameID+".json"), body, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = Load(dir, filenameID)
	if err == nil {
		t.Fatal("Load accepted a session whose decoded id differs from its filename")
	}
	if !strings.Contains(err.Error(), "session id") {
		t.Fatalf("Load error = %v, want a session-id diagnosis", err)
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Load reported a missing file instead of validating its contents: %v", err)
	}
}
