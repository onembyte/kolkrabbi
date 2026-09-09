package session

import (
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
	ID        string
	Title     string
	Model     string
	Effort    string
	Connector string
	CWD       string
	Updated   time.Time
	State     State
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

// Overview lists every session in dir, newest first, with whether each one is
// being run right now.
//
// The reading is List's: a header per session, and a full decode only for a
// session written before headers existed (OPTIMIZATION_PLAN.md O6). Liveness
// is the one thing no header can hold — it is a fact about a running process,
// not about a file — so it is asked per card, which is a lock probe and not a
// read.
func Overview(dir string) ([]Card, error) {
	metas, err := List(dir)
	if err != nil {
		return nil, err
	}
	cards := make([]Card, 0, len(metas))
	for _, m := range metas {
		cards = append(cards, Card{
			ID:        m.ID,
			Title:     m.Title,
			Model:     m.Model,
			Effort:    m.Effort,
			Connector: m.Connector,
			CWD:       m.CWD,
			Updated:   m.UpdatedAt,
			State:     Live(dir, m.ID),
		})
	}
	return cards, nil
}
