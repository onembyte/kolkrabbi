package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Card is one session as a list shows it: who and when, never what was said.
//
// Deliberately not a *Session. Loading a megabyte of transcript to render one
// line is the difference between a list that can be polled and one that cannot,
// and a type that cannot carry a transcript cannot accidentally leak one into a
// view.
type Card struct {
	ID      string
	Title   string
	Model   string
	CWD     string
	Updated time.Time
	State   State
}

// Name is what to show for a session, titled or not.
//
// A card with an empty name is a card nobody can pick out of a list, and an
// untitled session is the normal state until the fast lane names one.
func (c Card) Name() string {
	if title := strings.TrimSpace(c.Title); title != "" {
		return title
	}
	return c.ID
}

// cardFile is the subset of a session file a card needs.
//
// Messages is absent on purpose: encoding/json skips a field it has nowhere to
// put, so a transcript is walked but never allocated. That is the whole reason
// this type exists next to Session rather than reusing it.
type cardFile struct {
	ID        string    `json:"id"`
	Model     string    `json:"model"`
	Title     string    `json:"title"`
	CWD       string    `json:"cwd,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Overview lists every session in dir, newest first, with whether each one is
// being run right now.
func Overview(dir string) ([]Card, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	cards := make([]Card, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var file cardFile
		if err := json.Unmarshal(body, &file); err != nil {
			// One unreadable file must not cost the whole list. A session
			// mid-write looks exactly like a corrupt one for a few
			// milliseconds, and a dashboard that blanks when that happens is
			// worse than one that shows a session late.
			continue
		}
		id := file.ID
		if id == "" {
			id = strings.TrimSuffix(name, ".json")
		}
		cards = append(cards, Card{
			ID:      id,
			Title:   file.Title,
			Model:   file.Model,
			CWD:     file.CWD,
			Updated: file.UpdatedAt,
			State:   Live(dir, id),
		})
	}

	sort.Slice(cards, func(i, j int) bool { return cards[i].Updated.After(cards[j].Updated) })
	return cards, nil
}
