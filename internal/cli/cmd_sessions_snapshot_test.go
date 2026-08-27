package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/session"
)

// A per-turn snapshot layer is the first thing to suspect when a data directory
// grows, and the last thing anyone would guess. `kolk sessions` says so.
func TestSessionsReportsTheSnapshotStoreSize(t *testing.T) {
	a, stdout, _ := newTestApp(t, "")
	d, err := a.resolve()
	if err != nil {
		t.Fatal(err)
	}
	sess := session.New(d.Sessions(), "test/model")
	if err := sess.Save(); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(sess.CkptDir(), "shadow.git", "objects")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, "pack"), make([]byte, 40*1024), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := a.runSessions(context.Background(), nil); err != nil {
		t.Fatalf("runSessions: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "snap:") {
		t.Errorf("the listing never mentions the snapshot store:\n%s", out)
	}
	if !strings.Contains(out, "40") {
		t.Errorf("the listing does not report the store's size:\n%s", out)
	}
}

// A session with no snapshot store is the common case, and a column of zeroes
// teaches people to stop reading the line.
func TestSessionsSaysNothingAboutAStoreThatIsNotThere(t *testing.T) {
	a, stdout, _ := newTestApp(t, "")
	d, err := a.resolve()
	if err != nil {
		t.Fatal(err)
	}
	sess := session.New(d.Sessions(), "test/model")
	if err := sess.Save(); err != nil {
		t.Fatal(err)
	}
	if err := a.runSessions(context.Background(), nil); err != nil {
		t.Fatalf("runSessions: %v", err)
	}
	if strings.Contains(stdout.String(), "snap:") {
		t.Errorf("a session with no snapshots grew a snapshot column:\n%s", stdout.String())
	}
}

// Nothing outlives the thing it was a snapshot of. The deletion already removes
// the whole checkpoint directory, and this is the test that keeps it true when
// someone later replaces RemoveAll with a list of known filenames.
func TestDeletingASessionDeletesItsSnapshotStore(t *testing.T) {
	a, _, _ := newTestApp(t, "")
	d, err := a.resolve()
	if err != nil {
		t.Fatal(err)
	}
	dir := d.Sessions()
	sess := session.New(dir, "test/model")
	if err := sess.Save(); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(sess.CkptDir(), "shadow.git")
	if err := os.MkdirAll(filepath.Join(store, "objects", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, "HEAD"), []byte("ref: refs/heads/main\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := session.Delete(dir, sess.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(store); !os.IsNotExist(err) {
		t.Errorf("the snapshot store outlived the session it belonged to: %v", err)
	}
}
