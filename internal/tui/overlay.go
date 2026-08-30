package tui

// filterBox is the append/backspace-only text buffer a filterable overlay
// uses for its query line. It is deliberately not the composer's Editor: the
// composer needs cursor movement, multiline input and history, and a
// filterable overlay's Up/Down (and, in /model's case, Left/Right for the
// effort dial) are claimed for row navigation instead — there is no cursor
// position left over for the query line to move within. Typing always
// appends to the end, and Backspace always removes the end.
type filterBox struct {
	runes []rune
}

// String returns the query typed so far.
func (f *filterBox) String() string { return string(f.runes) }

// insert appends typed text to the end of the query.
func (f *filterBox) insert(text string) {
	f.runes = append(f.runes, []rune(text)...)
}

// backspace removes the last rune of the query. It reports whether anything
// was removed, so a caller can tell "the query got shorter" apart from "the
// query was already empty" — the difference between clearing a filter and
// closing the overlay it belongs to.
func (f *filterBox) backspace() bool {
	if len(f.runes) == 0 {
		return false
	}
	f.runes = f.runes[:len(f.runes)-1]
	return true
}

// scrollWindow returns the least-scrolled top row that still keeps selected
// visible inside a window of the given size — the suggestion dropdown's own
// rule (walking an arrow key one row at a time rather than paging under the
// cursor), extracted so a filterable overlay's own list scrolls the same way
// instead of carrying a second copy of the same three comparisons.
func scrollWindow(selected, top, window int) int {
	switch {
	case selected < top:
		top = selected
	case selected >= top+window:
		top = selected - window + 1
	}
	if top < 0 {
		top = 0
	}
	return top
}
