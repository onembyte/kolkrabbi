package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeSession(t *testing.T, dir, id string, body map[string]any) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestOverviewListsWhatACardNeeds(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, validTestSessionID, map[string]any{
		"id": validTestSessionID, "model": "vendor/model", "title": "fix the parser",
		"cwd": "/p", "updated_at": "2026-08-26T10:00:00Z",
		"messages": []map[string]string{{"role": "user", "content": "hello"}},
	})

	cards, err := Overview(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 {
		t.Fatalf("got %d cards", len(cards))
	}
	card := cards[0]
	if card.ID != validTestSessionID || card.Title != "fix the parser" || card.Model != "vendor/model" || card.CWD != "/p" {
		t.Fatalf("card = %+v", card)
	}
	if !card.Updated.Equal(time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("updated = %v", card.Updated)
	}
}

func TestOverviewDoesNotReadTheTranscript(t *testing.T) {
	dir := t.TempDir()
	// A card shows who and when, never what was said. Loading a megabyte of
	// transcript to render one line is the difference between a list that can
	// be polled and one that cannot.
	writeSession(t, dir, validTestSessionID, map[string]any{
		"id": validTestSessionID, "updated_at": "2026-08-26T10:00:00Z",
		"messages": []map[string]string{{"role": "user", "content": "secret contents"}},
	})

	cards, err := Overview(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 {
		t.Fatalf("got %d cards", len(cards))
	}
	// The type itself must not be able to carry a transcript.
	if _, ok := any(cards[0]).(interface{ Messages() []Message }); ok {
		t.Fatal("a card can carry messages")
	}
}

func TestOverviewIsNewestFirst(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, validTestSessionID, map[string]any{"id": validTestSessionID, "updated_at": "2026-08-20T10:00:00Z"})
	writeSession(t, dir, validTestSessionID2, map[string]any{"id": validTestSessionID2, "updated_at": "2026-08-26T10:00:00Z"})

	cards, _ := Overview(dir)

	if len(cards) != 2 || cards[0].ID != validTestSessionID2 {
		t.Fatalf("cards = %+v, want the newest first", cards)
	}
}

func TestOverviewSaysWhichSessionsAreRunning(t *testing.T) {
	if !lockingWorks() {
		t.Skip("advisory locks unsupported")
	}
	dir := t.TempDir()
	writeSession(t, dir, validTestSessionID, map[string]any{"id": validTestSessionID, "updated_at": "2026-08-26T10:00:00Z"})
	writeSession(t, dir, validTestSessionID2, map[string]any{"id": validTestSessionID2, "updated_at": "2026-08-25T10:00:00Z"})

	held, err := Hold(dir, validTestSessionID)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()

	cards, _ := Overview(dir)

	byID := map[string]State{}
	for _, card := range cards {
		byID[card.ID] = card.State
	}
	// The first question a control plane answers.
	if byID[validTestSessionID] != StateLive {
		t.Fatalf("live session = %v", byID[validTestSessionID])
	}
	if byID[validTestSessionID2] != StateIdle {
		t.Fatalf("idle session = %v", byID[validTestSessionID2])
	}
}

func TestOverviewSkipsWhatIsNotASession(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, validTestSessionID, map[string]any{"id": validTestSessionID, "updated_at": "2026-08-26T10:00:00Z"})
	if err := os.WriteFile(filepath.Join(dir, "s_bad.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, validTestSessionID+".ckpt"), 0o700); err != nil {
		t.Fatal(err)
	}

	cards, err := Overview(dir)
	if err != nil {
		t.Fatalf("one corrupt file failed the whole list: %v", err)
	}
	if len(cards) != 1 || cards[0].ID != validTestSessionID {
		t.Fatalf("cards = %+v", cards)
	}
}

func TestOverviewOfNothingIsNothing(t *testing.T) {
	cards, err := Overview(filepath.Join(t.TempDir(), "never-created"))
	if err != nil {
		t.Fatalf("a missing directory is not an error: %v", err)
	}
	if len(cards) != 0 {
		t.Fatalf("cards = %+v", cards)
	}
}

func TestASessionWithNoTitleStillIdentifiesItself(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, validTestSessionID, map[string]any{"id": validTestSessionID, "updated_at": "2026-08-26T10:00:00Z"})

	cards, _ := Overview(dir)

	// A card with an empty name is a card nobody can pick out of a list.
	if cards[0].Name() == "" {
		t.Fatalf("card = %+v, want a usable name", cards[0])
	}
}
