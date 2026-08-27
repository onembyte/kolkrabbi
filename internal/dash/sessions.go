package dash

import (
	"fmt"
	"html"
	"sort"
	"strings"
)

// SessionCard is one session as the page shows it.
//
// It is a view model, filled in by the caller, because this package renders and
// does not gather: the four sources behind these fields — the session header,
// the advisory lock, the journal tail and the usage log — were each built with
// their own cost decisions, and re-reading them from inside a template would
// undo those.
type SessionCard struct {
	ID        string
	Name      string
	Model     string
	CWD       string
	Live      bool
	BlockedOn string // the tool a stopped session is waiting on; empty if it is not
	Cost      float64
	CostKnown bool
}

// SharedCheckout is one directory more than one live session is working in.
type SharedCheckout struct {
	Dir      string
	Sessions []string
}

// renderSessionCards writes the cards, blocked ones first.
//
// Ordering is the feature. Item 27 calls blocked the decisive field: a session
// waiting on a permission prompt has stopped, is spending nothing, and needs a
// person — and it looks exactly like a session thinking hard. A view that
// cannot tell those apart lets work sit unnoticed for an hour, and a view that
// can but buries it on row nine is the same view.
func renderSessionCards(cards []SessionCard) string {
	if len(cards) == 0 {
		return ""
	}
	ordered := make([]SessionCard, len(cards))
	copy(ordered, cards)
	sort.SliceStable(ordered, func(i, j int) bool {
		if (ordered[i].BlockedOn != "") != (ordered[j].BlockedOn != "") {
			return ordered[i].BlockedOn != ""
		}
		return ordered[i].Live && !ordered[j].Live
	})

	var b strings.Builder
	b.WriteString(`<section><h2>Sessions</h2><div class="cards">`)
	for _, card := range ordered {
		class := "card"
		if card.BlockedOn != "" {
			class += " blocked"
		}
		fmt.Fprintf(&b, `<article class="%s">`, class)
		fmt.Fprintf(&b, `<h3>%s</h3>`, escape(card.Name))

		// The same words the listing uses. Two vocabularies for one set of
		// facts is how a user learns to trust neither.
		state := "idle"
		if card.Live {
			state = "live"
		}
		fmt.Fprintf(&b, `<p class="state">%s`, state)
		if card.BlockedOn != "" {
			fmt.Fprintf(&b, ` · <strong>blocked</strong> on %s`, escape(card.BlockedOn))
		}
		b.WriteString(`</p>`)

		fmt.Fprintf(&b, `<p class="meta">%s`, escape(card.Model))
		if card.CostKnown {
			fmt.Fprintf(&b, ` · %s`, money(card.Cost))
		}
		b.WriteString(`</p>`)
		if card.CWD != "" {
			fmt.Fprintf(&b, `<p class="meta">%s</p>`, escape(card.CWD))
		}
		fmt.Fprintf(&b, `<p class="meta">%s</p></article>`, escape(card.ID))
	}
	b.WriteString(`</div></section>`)
	return b.String()
}

// renderSharedCheckouts says when live sessions overlap, in the page's voice
// and the listing's words.
func renderSharedCheckouts(shared []SharedCheckout) string {
	if len(shared) == 0 {
		return ""
	}
	var b strings.Builder
	for _, s := range shared {
		fmt.Fprintf(&b,
			`<p class="warn">%d live sessions are working in the same directory (%s): %s — they will edit each other's files, and an /undo in one takes back what the other did.</p>`,
			len(s.Sessions), escape(s.Dir), escape(strings.Join(s.Sessions, ", ")))
	}
	return b.String()
}

// escape is applied to every value that came from outside this package. A
// session title is whatever the fast lane named it after reading a user's
// words, and a working directory is whatever the filesystem holds.
func escape(text string) string { return html.EscapeString(text) }
