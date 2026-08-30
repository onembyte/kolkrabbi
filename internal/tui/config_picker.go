package tui

import (
	"sort"
	"strconv"
	"strings"
)

// The /config picker is a searchable overlay over every setting, the same
// shape /model already has. It differs in one way that changes everything
// past filtering: picking a row is never itself a command to run — a setting
// still needs its value typed — so Enter fills the composer's draft with
// `/config set <key> ` instead of answering with something meant to run on
// its own the way the model picker's answer does.

// ConfigPick is the state of the open /config picker, for tests and adapters.
// Entries and Index describe the filtered, ranked view, matching ModelPick.
type ConfigPick struct {
	Entries []SettingSpec
	Index   int
	Filter  string
}

// RequestConfigPicker opens the picker over the given settings.
func (c *Controller) RequestConfigPicker(entries []SettingSpec) {
	if len(entries) == 0 {
		return
	}
	c.configPicker = make([]SettingSpec, len(entries))
	copy(c.configPicker, entries)
	c.configIndex = 0
	c.configFilter = filterBox{}
	c.beforeQuestion = c.status.Lifecycle
	c.setLifecycle("question")
}

// ConfigPicker reports the open picker's filtered rows and where its marker
// sits, for tests and adapters. Nil when no picker is open.
func (c *Controller) ConfigPicker() *ConfigPick {
	if c.configPicker == nil {
		return nil
	}
	indices := c.filteredConfigIndices()
	entries := make([]SettingSpec, len(indices))
	for row, index := range indices {
		entries[row] = c.configPicker[index]
	}
	return &ConfigPick{Entries: entries, Index: c.configIndex, Filter: c.configFilter.String()}
}

// filteredConfigIndices ranks configPicker's own indices by how well they
// match the current filter, best first — indices rather than copies, for the
// same reason filteredModelIndices uses them: a row's position in the
// filtered view must not be confused with its position in the catalog it was
// opened with.
func (c *Controller) filteredConfigIndices() []int {
	filter := c.configFilter.String()
	type scoredIndex struct{ index, score int }
	matches := make([]scoredIndex, 0, len(c.configPicker))
	for index, entry := range c.configPicker {
		score, ok := fuzzyScoreFields([]string{entry.Key, entry.Summary, entry.Value}, filter)
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

// resolveConfigPicker closes the picker. A non-empty key fills the composer's
// draft with the set command for it, exactly as picking the same row from the
// inline suggestion menu already does (completeSuggestion) — one behavior for
// "a setting was chosen," however it was reached. Nothing is submitted: the
// value is still the user's to type.
func (c *Controller) resolveConfigPicker(key string) Effect {
	if key != "" {
		completion := "/config set " + key + " "
		c.editor.setDraft(completion)
		c.screen.SetDraft(completion)
	}
	c.configPicker = nil
	c.configIndex = 0
	c.configFilter = filterBox{}
	c.setLifecycle(c.beforeQuestion)
	c.beforeQuestion = ""
	return Effect{PickConfig: key, PickDismissed: key == ""}
}

func (c *Controller) handleConfigPickerKey(key Key) Effect {
	indices := c.filteredConfigIndices()
	count := len(indices)
	switch key.Kind {
	case KeyInterrupt, KeyEOF:
		return c.resolveConfigPicker("")
	case KeyEscape:
		// Back out one step at a time, matching /model: an active filter is
		// cleared first, and the overlay itself only closes once there is
		// nothing left to clear.
		if c.configFilter.String() != "" {
			c.configFilter = filterBox{}
			c.configIndex = 0
			return Effect{}
		}
		return c.resolveConfigPicker("")
	case KeyText:
		c.configFilter.insert(key.Text)
		c.configIndex = 0
		return Effect{}
	case KeyBackspace:
		if c.configFilter.backspace() {
			c.configIndex = 0
		}
		return Effect{}
	case KeyUp:
		if count == 0 {
			return Effect{}
		}
		c.configIndex = (c.configIndex - 1 + count) % count
		return Effect{}
	case KeyDown:
		if count == 0 {
			return Effect{}
		}
		c.configIndex = (c.configIndex + 1) % count
		return Effect{}
	case KeyEnter:
		if count == 0 {
			return Effect{}
		}
		return c.resolveConfigPicker(c.configPicker[indices[c.configIndex]].Key)
	}
	return Effect{}
}

// configPickerLines draws the picker. The value in effect is shown on the
// row, so "what is my effort set to" is answered by opening the list rather
// than leaving the session to run `kolk config`.
func (c *Controller) configPickerLines(width int) []string {
	lines := []string{
		horizontalRule("config", width),
		clipLine("filter settings — type to filter, ↑/↓ choose, Enter to edit", width),
	}
	filter := c.configFilter.String()
	if filter == "" {
		lines = append(lines, clipLine("filter: (type to narrow the list)", width))
	} else {
		lines = append(lines, clipLine("filter: "+sanitizeTerminalLine(filter), width))
	}
	indices := c.filteredConfigIndices()
	if len(indices) == 0 {
		lines = append(lines, clipLine("no settings match this filter", width))
	}
	for row, index := range indices {
		entry := c.configPicker[index]
		marker := "  "
		if row == c.configIndex {
			marker = "> "
		}
		value := entry.Value
		if entry.Default {
			value += " (default)"
		}
		line := marker + strconv.Itoa(row+1) + "  " + sanitizeTerminalLine(entry.Key) +
			"  " + sanitizeTerminalLine(value)
		lines = append(lines, clipLine(line, width))
	}
	return append(lines, strings.Repeat("─", max(0, width)))
}
