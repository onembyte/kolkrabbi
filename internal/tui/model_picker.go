package tui

import (
	"sort"
	"strconv"
	"strings"
)

// The /model picker is an overlay over every model the session can reach —
// subscriptions first, then free, then local, then metered — with the effort
// dial of the selected model on the same row. Up and down choose a model;
// left and right turn its effort level, exactly the way the question picker
// lets the number keys pick a row outright.
//
// It reuses the question picker's lifecycle and Effect plumbing rather than
// growing a parallel one: one overlay open at a time, one way to resolve it,
// and the same Escape semantics throughout.

// ModelPickEntry is one row of the picker. ID and Name are the same fields a
// ModelSpec suggestion carries; Efforts is the ladder of effort levels the
// model's plan offers, empty for models where effort is not a dial kolk sets
// on the provider.
type ModelPickEntry struct {
	ID   string
	Name string
	// Efforts is ordered least to most. Empty means the row has no effort to
	// cycle, and left and right are dead keys for it.
	Efforts []string
	// Effort is where the dial sits for this entry; an index into Efforts.
	Effort int
}

// ModelPick is the state of the open /model picker, for tests and adapters.
// Entries and Index describe the filtered, ranked view — what is actually on
// screen — not the full catalog the picker was opened with.
type ModelPick struct {
	Entries []ModelPickEntry
	// Index is the highlighted row, 0-based, within Entries.
	Index int
	// Filter is the query typed so far; empty shows every row.
	Filter string
}

// RequestModelPicker opens the picker over the given rows. The first row is
// preselected — subscriptions sort first, so the cheapest working answer is
// already under the marker.
func (c *Controller) RequestModelPicker(entries []ModelPickEntry) {
	if len(entries) == 0 {
		return
	}
	c.modelPicker = make([]ModelPickEntry, len(entries))
	copy(c.modelPicker, entries)
	c.modelIndex = 0
	c.modelFilter = filterBox{}
	c.modelTop = 0
	c.beforeQuestion = c.status.Lifecycle
	c.setLifecycle("question")
}

// ModelPicker reports the open picker's filtered rows and where its marker
// sits, for tests and adapters. Nil when no picker is open.
func (c *Controller) ModelPicker() *ModelPick {
	if c.modelPicker == nil {
		return nil
	}
	indices := c.filteredModelIndices()
	entries := make([]ModelPickEntry, len(indices))
	for row, index := range indices {
		entries[row] = c.modelPicker[index]
	}
	return &ModelPick{Entries: entries, Index: c.modelIndex, Filter: c.modelFilter.String()}
}

// filteredModelIndices ranks modelPicker's own indices by how well they match
// the current filter, best first — indices rather than copies, because the
// effort dial mutates a row in modelPicker itself, and a row's position in
// the filtered view must not be confused with its position in the catalog it
// was opened with.
func (c *Controller) filteredModelIndices() []int {
	filter := c.modelFilter.String()
	type scoredIndex struct{ index, score int }
	matches := make([]scoredIndex, 0, len(c.modelPicker))
	for index, entry := range c.modelPicker {
		score, ok := fuzzyScoreFields([]string{entry.ID, entry.Name}, filter)
		if !ok {
			continue
		}
		matches = append(matches, scoredIndex{index: index, score: score})
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].score > matches[j].score })
	indices := make([]int, len(matches))
	for row, match := range matches {
		indices[row] = match.index
	}
	return indices
}

// resolveModelPicker closes the picker. answers == nil is the dismissal; a
// command string is dispatched by the surface like any other submitted line.
func (c *Controller) resolveModelPicker(answer string) Effect {
	c.modelPicker = nil
	c.modelIndex = 0
	c.modelFilter = filterBox{}
	c.modelTop = 0
	c.setLifecycle(c.beforeQuestion)
	c.beforeQuestion = ""
	return Effect{PickModel: answer, PickDismissed: answer == ""}
}

func (c *Controller) handleModelPickerKey(key Key) Effect {
	indices := c.filteredModelIndices()
	count := len(indices)
	switch key.Kind {
	case KeyInterrupt, KeyEOF:
		return c.resolveModelPicker("")
	case KeyEscape:
		// Back out one step at a time, the way fzf and every fuzzy picker this
		// leaf is meant to match does: an active filter is cleared first, and
		// the overlay itself only closes once there is nothing left to clear.
		if c.modelFilter.String() != "" {
			c.modelFilter = filterBox{}
			c.modelIndex = 0
			c.modelTop = 0
			return Effect{}
		}
		return c.resolveModelPicker("")
	case KeyText, KeyPaste:
		c.modelFilter.insert(key.Text)
		c.modelIndex = 0
		c.modelTop = 0
		return Effect{}
	case KeyBackspace:
		if c.modelFilter.backspace() {
			c.modelIndex = 0
			c.modelTop = 0
		}
		return Effect{}
	case KeyUp:
		if count == 0 {
			return Effect{}
		}
		c.modelIndex = (c.modelIndex - 1 + count) % count
		c.modelTop = scrollWindow(c.modelIndex, c.modelTop, c.windowSize())
		return Effect{}
	case KeyDown:
		if count == 0 {
			return Effect{}
		}
		c.modelIndex = (c.modelIndex + 1) % count
		c.modelTop = scrollWindow(c.modelIndex, c.modelTop, c.windowSize())
		return Effect{}
	case KeyLeft, KeyRight:
		if count == 0 {
			return Effect{}
		}
		entry := &c.modelPicker[indices[c.modelIndex]]
		if len(entry.Efforts) == 0 {
			return Effect{}
		}
		step := 1
		if key.Kind == KeyLeft {
			step = -1
		}
		entry.Effort = (entry.Effort + step + len(entry.Efforts)) % len(entry.Efforts)
		return Effect{}
	case KeyEnter:
		if count == 0 {
			return Effect{}
		}
		return c.resolveModelPicker(modelPickAnswer(c.modelPicker[indices[c.modelIndex]]))
	}
	return Effect{}
}

// modelPickAnswer is the whole line the selection turns into: one command,
// ready to run, so the picker's answer needs no second interpretation.
func modelPickAnswer(entry ModelPickEntry) string {
	command := "/model " + entry.ID
	if len(entry.Efforts) > 0 {
		command += " " + entry.Efforts[entry.Effort]
	}
	return command
}

// modelPickerLines draws the picker. The hint line names the keys, because
// left and right doing something here — where they do nothing in the rest of
// the composer — is a fact nobody would guess.
func (c *Controller) modelPickerLines(width int) []string {
	lines := []string{
		horizontalRule("model", width),
		clipLine("switch model — type to filter, ↑/↓ choose, ←/→ effort, Enter to switch", width),
	}
	filter := c.modelFilter.String()
	if filter == "" {
		lines = append(lines, clipLine("filter: (type to narrow the list)", width))
	} else {
		lines = append(lines, clipLine("filter: "+sanitizeTerminalLine(filter), width))
	}
	indices := c.filteredModelIndices()
	if len(indices) == 0 {
		lines = append(lines, clipLine("no models match this filter", width))
	}
	// Only the window is drawn, the same rule the suggestion dropdown already
	// follows: a catalog longer than it would otherwise render every row
	// unbounded, which is exactly the defect scrollWindow exists to prevent.
	window := c.windowSize()
	first, last := 0, len(indices)
	if window > 0 && len(indices) > window {
		first = min(max(0, c.modelTop), max(0, len(indices)-1))
		last = min(len(indices), first+window)
	}
	if first > 0 {
		lines = append(lines, clipLine("  ↑", width))
	}
	for row := first; row < last; row++ {
		index := indices[row]
		entry := c.modelPicker[index]
		marker := "  "
		if row == c.modelIndex {
			marker = "> "
		}
		line := marker + strconv.Itoa(row+1) + "  " + sanitizeTerminalLine(entry.ID) +
			"  " + sanitizeTerminalLine(entry.Name)
		line += "  " + effortDial(entry)
		lines = append(lines, clipLine(line, width))
	}
	if last < len(indices) {
		lines = append(lines, clipLine("  ↓", width))
	}
	return append(lines, strings.Repeat("─", max(0, width)))
}

// effortDial renders one row's effort ladder with the active level bracketed:
// [low] medium  high. A row without efforts draws nothing.
func effortDial(entry ModelPickEntry) string {
	if len(entry.Efforts) == 0 {
		return ""
	}
	pieces := make([]string, len(entry.Efforts))
	for index, effort := range entry.Efforts {
		if index == entry.Effort {
			pieces[index] = "[" + effort + "]"
		} else {
			pieces[index] = effort
		}
	}
	return strings.Join(pieces, " ")
}
