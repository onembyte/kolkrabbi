package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/continuity"
)

// A save leaves a header beside the transcript: what every listing shows,
// without the transcript it shows it for.
func TestSaveWritesTheHeaderBesideTheTranscript(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "vendor/model")
	s.Title = "fix the parser"
	s.CWD = "/p"
	s.Effort = "high"
	s.Connector = "codex"
	s.AppendMessage(toProvider(Message{Role: "user", Content: "hello"}))
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	m, ok := readMeta(dir, s.ID)
	if !ok {
		t.Fatalf("no header at %s", metaPath(dir, s.ID))
	}
	if m.ID != s.ID || m.Title != "fix the parser" || m.Model != "vendor/model" || m.CWD != "/p" {
		t.Fatalf("header = %+v", m)
	}
	if m.Effort != "high" || m.Connector != "codex" {
		t.Fatalf("header lost the dial or the connector: %+v", m)
	}
	if m.MessageCount != 1 {
		t.Fatalf("header counts %d messages, want 1", m.MessageCount)
	}
	if !m.UpdatedAt.Equal(s.UpdatedAt) {
		t.Fatalf("header updated %v, transcript %v", m.UpdatedAt, s.UpdatedAt)
	}
	// A header is a header: it must stay small enough that writing one per
	// save is free.
	info, err := os.Stat(metaPath(dir, s.ID))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > 2048 {
		t.Fatalf("header is %d bytes; it is meant to be a few hundred", info.Size())
	}
}

// The point of the header: a listing that does not decode transcripts. Proved
// by leaving a transcript no decoder could read — if the listing still names
// the session, it never went near it.
func TestListReadsHeadersAndNotTranscripts(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "vendor/model")
	s.Title = "still listed"
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, s.ID+".json"), []byte("{ this is not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	all, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].Title != "still listed" {
		t.Fatalf("list = %+v, want the header's row", all)
	}
}

// A session written before headers existed is listed exactly as it was: one
// decode, and every field a card needs.
func TestASessionWithNoHeaderIsStillListed(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, validTestSessionID, map[string]any{
		"id": validTestSessionID, "model": "vendor/old", "title": "before headers",
		"cwd": "/p", "effort": "low", "connector": "claude",
		"updated_at": "2026-08-26T10:00:00Z",
		"messages": []map[string]string{
			{"role": "user", "content": "one"}, {"role": "assistant", "content": "two"},
		},
	})

	all, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("list = %+v", all)
	}
	m := all[0]
	if m.ID != validTestSessionID || m.Title != "before headers" || m.Model != "vendor/old" || m.CWD != "/p" {
		t.Fatalf("derived header = %+v", m)
	}
	if m.Effort != "low" || m.Connector != "claude" || m.MessageCount != 2 {
		t.Fatalf("derived header = %+v, want the dial, the connector and two messages", m)
	}
	if !m.UpdatedAt.Equal(time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("derived header updated %v", m.UpdatedAt)
	}
}

// A paused session is one `kolk doctor` must name, and asking every transcript
// on the machine is what O6 removed — so the header carries the pause.
func TestTheHeaderCarriesAPause(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "vendor/model")
	reset := time.Now().Add(time.Hour).Round(time.Second)
	s.SetPaused(&continuity.Pause{Kind: "subscription_allowance", Scope: "plan", ResetAt: reset})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	all, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].Pause == nil || !all[0].Pause.ResetAt.Equal(reset) {
		t.Fatalf("list = %+v, want the pause in the header", all)
	}
}

// Deleting a session takes its header with it. A header left behind is a
// session that still appears in every listing on the machine.
func TestDeleteRemovesTheHeader(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "m")
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if err := Delete(dir, s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(metaPath(dir, s.ID)); !os.IsNotExist(err) {
		t.Fatalf("header after delete: %v", err)
	}
	all, _ := List(dir)
	if len(all) != 0 {
		t.Fatalf("list after delete = %+v", all)
	}
}

// The header is derived state, so something must be able to prove it still
// matches its transcript and put it back when it does not. That is doctor's
// half of the bargain.
func TestRepairMetaRewritesADriftedHeader(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "m")
	s.Title = "the real title"
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	if repaired, err := RepairMeta(dir, s.ID); err != nil || repaired {
		t.Fatalf("RepairMeta on a fresh save = (%v, %v), want nothing to do", repaired, err)
	}

	drifted := Meta{Version: metaVersion, ID: s.ID, Title: "a stale title", UpdatedAt: s.UpdatedAt}
	body, err := json.Marshal(drifted)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metaPath(dir, s.ID), append(body, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	repaired, err := RepairMeta(dir, s.ID)
	if err != nil || !repaired {
		t.Fatalf("RepairMeta on a drifted header = (%v, %v), want a repair", repaired, err)
	}
	if m, ok := readMeta(dir, s.ID); !ok || m.Title != "the real title" {
		t.Fatalf("header after repair = %+v (%v)", m, ok)
	}
}

// Resuming decodes one transcript: the one it resumes.
func TestLatestForDirReturnsTheWholeChosenSession(t *testing.T) {
	dir := t.TempDir()
	here := filepath.Join(dir, "project")
	elsewhere := New(dir, "m")
	elsewhere.CWD = filepath.Join(dir, "other")
	if err := elsewhere.Save(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond) // distinct UpdatedAt
	mine := New(dir, "m")
	mine.CWD = here
	mine.AppendMessage(toProvider(Message{Role: "user", Content: "resume me"}))
	if err := mine.Save(); err != nil {
		t.Fatal(err)
	}

	got, err := LatestForDir(dir, here)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ID != mine.ID {
		t.Fatalf("LatestForDir = %v, want %s", got, mine.ID)
	}
	if len(got.Messages) != 1 || got.Messages[0].Content != "resume me" {
		t.Fatalf("resumed session carries %d messages; a resume needs the transcript", len(got.Messages))
	}
}

// A header nothing wrote for another session, or written by a kolk that means
// something else by it, is not read.
func TestAHeaderThatIsNotThisSessionsIsIgnored(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "m")
	s.Title = "from the transcript"
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{"version":1,"id":"` + validTestSessionID2 + `","title":"another session"}`,
		`{"version":99,"id":"` + s.ID + `","title":"another version"}`,
		`{ not json`,
	} {
		if err := os.WriteFile(metaPath(dir, s.ID), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		all, err := List(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 1 || all[0].Title != "from the transcript" {
			t.Fatalf("header %q gave %+v, want the transcript's own row", body, all)
		}
	}
}
