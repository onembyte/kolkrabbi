package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/session"
	"github.com/onembyte/kolkrabbi/internal/stats"
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

// Two live sessions in one checkout is a thing people do on purpose, and a
// thing they should be told about once — not discover when an /undo restores
// someone else's work.
func TestSessionsWarnsAboutASharedCheckout(t *testing.T) {
	a, stdout, _ := newTestApp(t, "")
	d, err := a.resolve()
	if err != nil {
		t.Fatal(err)
	}
	dir := d.Sessions()

	// Two sessions in one directory, both held, so both are live.
	shared := t.TempDir()
	for _, id := range []string{"first", "second"} {
		sess := session.New(dir, "test/model")
		sess.CWD = shared
		sess.Title = id
		if err := sess.Save(); err != nil {
			t.Fatal(err)
		}
		held, err := session.Hold(dir, sess.ID)
		if err != nil {
			t.Skipf("advisory locks unavailable here: %v", err)
		}
		t.Cleanup(func() { _ = held.Close() })
	}
	// Both sessions share one directory: rewrite the second to match the first.
	cards, err := session.Overview(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 2 {
		t.Fatalf("expected two cards, got %d", len(cards))
	}

	if err := a.runSessions(context.Background(), nil); err != nil {
		t.Fatalf("runSessions: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "same directory") {
		t.Errorf("the listing does not warn about a shared checkout:\n%s", out)
	}
}

// A session that has stopped at a prompt is the one thing worth seeing when
// scanning a list, so the listing says it before anything else.
func TestSessionsReportsABlockedSession(t *testing.T) {
	a, stdout, _ := newTestApp(t, "")
	d, err := a.resolve()
	if err != nil {
		t.Fatal(err)
	}
	dir := d.Sessions()

	sess := session.New(dir, "test/model")
	sess.Title = "the blocked one"
	if err := sess.Save(); err != nil {
		t.Fatal(err)
	}
	held, err := session.Hold(dir, sess.ID)
	if err != nil {
		t.Skipf("advisory locks unavailable here: %v", err)
	}
	t.Cleanup(func() { _ = held.Close() })

	line := `{"seq":1,"ts":"2026-08-27T18:00:00Z","session":"` + sess.ID +
		`","turn":"t_01ARYZ6S41TSV4RRFFQ69G5FAW","type":"permission.requested",` +
		`"data":{"id":"p1","tool":"bash","detail":"rm -rf ./build"}}`
	if err := os.WriteFile(filepath.Join(dir, sess.ID+".events.ndjson"), []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := a.runSessions(context.Background(), nil); err != nil {
		t.Fatalf("runSessions: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{"waiting for you", "bash", "rm -rf ./build", sess.ID} {
		if !strings.Contains(out, want) {
			t.Errorf("the listing omits %q:\n%s", want, out)
		}
	}
}

// Cost is a number people act on: a session that has quietly spent four dollars
// is one somebody stops or looks into.
func TestSessionsShowsWhatASessionHasSpent(t *testing.T) {
	a, stdout, _ := newTestApp(t, "")
	d, err := a.resolve()
	if err != nil {
		t.Fatal(err)
	}
	sess := session.New(d.Sessions(), "test/model")
	if err := sess.Save(); err != nil {
		t.Fatal(err)
	}
	if err := stats.Append(d.Data, stats.Record{
		Kind: "call", Time: time.Now(), Session: sess.ID, Cost: 4.20,
	}); err != nil {
		t.Fatal(err)
	}

	if err := a.runSessions(context.Background(), nil); err != nil {
		t.Fatalf("runSessions: %v", err)
	}
	if !strings.Contains(stdout.String(), "$4.20") {
		t.Errorf("the listing does not show what the session spent:\n%s", stdout.String())
	}
}

// "Nothing recorded" and "ran free" are different facts, and only the second is
// worth a column. A session with no calls must not read as costing nothing.
func TestSessionsSaysNothingAboutASessionWithNoCalls(t *testing.T) {
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
	if strings.Contains(stdout.String(), "$0.00") {
		t.Errorf("a session with no recorded calls was reported as costing nothing:\n%s", stdout.String())
	}
}
