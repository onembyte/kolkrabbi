package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func journal(t *testing.T, dir, id string, lines ...string) {
	t.Helper()
	path := filepath.Join(dir, id+".events.ndjson")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func requested(seq int, permissionID, tool string) string {
	return fmt.Sprintf(`{"seq":%d,"ts":"2026-08-27T18:00:00Z","session":"s_x","turn":"t_x","type":"permission.requested","data":{"id":%q,"tool":%q,"detail":"d"}}`,
		seq, permissionID, tool)
}

func resolved(seq int, permissionID string) string {
	return fmt.Sprintf(`{"seq":%d,"ts":"2026-08-27T18:00:01Z","session":"s_x","turn":"t_x","type":"permission.resolved","data":{"id":%q,"decision":"allow_once"}}`,
		seq, permissionID)
}

func other(seq int) string {
	return fmt.Sprintf(`{"seq":%d,"ts":"2026-08-27T18:00:02Z","session":"s_x","turn":"t_x","type":"message.delta","data":{"text":"x"}}`, seq)
}

// A request with no matching resolution is a session that has stopped and is
// waiting for a person. That is the one thing a card must say.
func TestBlockedOnFindsAnUnansweredPrompt(t *testing.T) {
	dir := t.TempDir()
	journal(t, dir, "s_a", requested(1, "p1", "bash"), other(2))

	blocked, ok := BlockedOn(dir, "s_a")
	if !ok {
		t.Fatal("an unanswered permission request was not reported as blocking")
	}
	if blocked.Tool != "bash" {
		t.Errorf("tool = %q, want bash", blocked.Tool)
	}
}

func TestBlockedOnIgnoresAnAnsweredPrompt(t *testing.T) {
	dir := t.TempDir()
	journal(t, dir, "s_a", requested(1, "p1", "bash"), resolved(2, "p1"), other(3))

	if _, ok := BlockedOn(dir, "s_a"); ok {
		t.Error("an answered prompt still counted as blocking")
	}
}

// The resolution can arrive for an earlier request while a later one is still
// open — answering one prompt does not unblock the next.
func TestBlockedOnMatchesRequestsToTheirOwnResolutions(t *testing.T) {
	dir := t.TempDir()
	journal(t, dir, "s_a",
		requested(1, "p1", "bash"),
		requested(2, "p2", "write_file"),
		resolved(3, "p1"),
	)
	blocked, ok := BlockedOn(dir, "s_a")
	if !ok {
		t.Fatal("a still-open second request was not reported")
	}
	if blocked.Tool != "write_file" {
		t.Errorf("tool = %q, want the unanswered request's tool", blocked.Tool)
	}
}

func TestBlockedOnSaysNothingWithoutAJournal(t *testing.T) {
	if _, ok := BlockedOn(t.TempDir(), "s_missing"); ok {
		t.Error("a session with no journal was reported as blocked")
	}
}

// A malformed line costs its own line, not the answer. A journal is appended to
// by a live process, so the last line can be half-written at the moment it is
// read.
func TestBlockedOnSurvivesAHalfWrittenLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s_a.events.ndjson")
	body := requested(1, "p1", "bash") + "\n" + `{"seq":2,"type":"message.delta","dat`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := BlockedOn(dir, "s_a"); !ok {
		t.Error("a truncated final line hid an open permission request")
	}
}

// Only the tail is read. I27.2 made this listing cheap on purpose, and a full
// decode per session would undo it.
func TestBlockedOnReadsOnlyTheTail(t *testing.T) {
	dir := t.TempDir()
	lines := []string{requested(1, "old", "bash"), resolved(2, "old")}
	for seq := 3; seq < 20000; seq++ {
		lines = append(lines, other(seq))
	}
	lines = append(lines, requested(20000, "p9", "write_file"))
	journal(t, dir, "s_big", lines...)

	info, err := os.Stat(filepath.Join(dir, "s_big.events.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() < 2*blockedTailBytes {
		t.Fatalf("the fixture is %d bytes, too small to prove a tail read", info.Size())
	}
	blocked, ok := BlockedOn(dir, "s_big")
	if !ok {
		t.Fatal("the last request was not found in a large journal")
	}
	if blocked.Tool != "write_file" {
		t.Errorf("tool = %q, want the request at the end", blocked.Tool)
	}
}
