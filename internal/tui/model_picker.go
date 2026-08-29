package tui

import (
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
type ModelPick struct {
	Entries []ModelPickEntry
	// Index is the highlighted row, 0-based.
	Index int
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
	c.beforeQuestion = c.status.Lifecycle
	c.setLifecycle("question")
}

// ModelPicker reports the open picker's rows and where its marker sits, for
// tests and adapters. Nil when no picker is open.
func (c *Controller) ModelPicker() *ModelPick {
	if c.modelPicker == nil {
		return nil
	}
	entries := make([]ModelPickEntry, len(c.modelPicker))
	copy(entries, c.modelPicker)
	return &ModelPick{Entries: entries, Index: c.modelIndex}
}

// resolveModelPicker closes the picker. answers == nil is the dismissal; a
// command string is dispatched by the surface like any other submitted line.
func (c *Controller) resolveModelPicker(answer string) Effect {
	c.modelPicker = nil
	c.modelIndex = 0
	c.setLifecycle(c.beforeQuestion)
	c.beforeQuestion = ""
	return Effect{PickModel: answer, PickDismissed: answer == ""}
}

func (c *Controller) handleModelPickerKey(key Key) Effect {
	count := len(c.modelPicker)
	switch key.Kind {
	case KeyInterrupt, KeyEscape, KeyEOF:
		return c.resolveModelPicker("")
	case KeyUp:
		c.modelIndex = (c.modelIndex - 1 + count) % count
		return Effect{}
	case KeyDown:
		c.modelIndex = (c.modelIndex + 1) % count
		return Effect{}
	case KeyLeft, KeyRight:
		entry := &c.modelPicker[c.modelIndex]
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
		return c.resolveModelPicker(modelPickAnswer(c.modelPicker[c.modelIndex]))
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

// modelPickerLines draws the picker. The last line names the keys, because
// left and right doing something here — where they do nothing in the rest of
// the composer — is a fact nobody would guess.
func (c *Controller) modelPickerLines(width int) []string {
	lines := []string{
		horizontalRule("model", width),
		clipLine("switch model — up/down choose, ←/→ effort, Enter to switch", width),
	}
	for index, entry := range c.modelPicker {
		marker := "  "
		if index == c.modelIndex {
			marker = "> "
		}
		row := marker + strconv.Itoa(index+1) + "  " + sanitizeTerminalLine(entry.ID) +
			"  " + sanitizeTerminalLine(entry.Name)
		row += "  " + effortDial(entry)
		lines = append(lines, clipLine(row, width))
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
