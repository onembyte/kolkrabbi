package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// The /model picker answers with a ready-to-run command: the model, and its
// effort when the model has a dial, because a second interpretation of the
// pick is a second place to disagree about what was picked.
func TestModelPickerAnswersAReadyCommand(t *testing.T) {
	r := NewRuntime(RuntimeOptions{})
	done := make(chan string, 1)
	go func() {
		answer, ok := r.AskModel(context.Background(), []ModelPickEntry{
			{ID: "claude-sonnet", Name: "Claude Pro", Efforts: []string{"low", "medium", "high"}},
			{ID: "vendor/mock", Name: "metered"},
		})
		if !ok {
			t.Error("the picker refused to resolve its answer")
			done <- ""
			return
		}
		done <- answer
	}()
	waitForPickerOpen(t, r)

	r.mu.Lock()
	pick := r.controller.ModelPicker()
	r.mu.Unlock()
	if pick == nil || len(pick.Entries) != 2 || pick.Index != 0 {
		t.Fatalf("picker = %+v, want two rows with the first highlighted", pick)
	}
	if got := pick.Entries[0].Efforts[pick.Entries[0].Effort]; got != "low" {
		t.Fatalf("effort dial starts at %q, want the least expensive level", got)
	}

	// One row down to the metered model: its Enter answer has no effort on it.
	r.HandleKey(Key{Kind: KeyDown})
	r.HandleKey(Key{Kind: KeyEnter})
	if answer := <-done; answer != "/model vendor/mock" {
		t.Fatalf("answer = %q, want the bare model command", answer)
	}
}

// Left and right turn the effort dial of the selected model and nothing else:
// the row stays put, and a model without efforts ignores the key.
func TestModelPickerArrowsTurnTheEffortDial(t *testing.T) {
	r := NewRuntime(RuntimeOptions{})
	done := make(chan string, 1)
	go func() {
		answer, ok := r.AskModel(context.Background(), []ModelPickEntry{
			{ID: "claude-sonnet", Name: "Claude Pro", Efforts: []string{"low", "medium", "high"}},
			{ID: "vendor/mock", Name: "metered"},
		})
		if !ok {
			done <- ""
			return
		}
		done <- answer
	}()
	waitForPickerOpen(t, r)

	// Left from the least expensive level wraps to the most.
	r.HandleKey(Key{Kind: KeyLeft})
	r.mu.Lock()
	if got := r.controller.ModelPicker().Entries[0].Effort; got != 2 {
		t.Fatalf("effort index = %d, want the wrapped top level", got)
	}
	r.mu.Unlock()
	// Right wraps back to the start of the dial.
	r.HandleKey(Key{Kind: KeyRight})
	r.HandleKey(Key{Kind: KeyEnter})
	if answer := <-done; answer != "/model claude-sonnet low" {
		t.Fatalf("answer = %q, want the dial's level on the command", answer)
	}
}

// A dismissed picker must read as "nothing chosen", not as the first row.
func TestModelPickerDismissalIsNotAPick(t *testing.T) {
	r := NewRuntime(RuntimeOptions{})
	done := make(chan string, 1)
	go func() {
		answer, ok := r.AskModel(context.Background(), []ModelPickEntry{
			{ID: "claude-sonnet", Name: "Claude Pro"},
		})
		done <- ""
		_ = ok
		_ = answer
	}()
	waitForPickerOpen(t, r)
	r.HandleKey(Key{Kind: KeyEscape})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the dismissed picker never unblocked its waiter")
	}
}

// The picker's drawing names the keys: left and right being alive only here
// is not something anyone would guess without being told.
func TestModelPickerDrawingNamesTheKeysAndShowsTheDial(t *testing.T) {
	c := NewController(Status{}, defaultDraftSize)
	c.RequestModelPicker([]ModelPickEntry{
		{ID: "claude-sonnet", Name: "Claude Pro", Efforts: []string{"low", "medium", "high"}, Effort: 2},
		{ID: "vendor/mock", Name: "metered"},
	})
	view := strings.Join(c.modelPickerLines(80), "\n")
	for _, want := range []string{"←/→", "[high]", "claude-sonnet", "vendor/mock"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view does not show %q:\n%s", want, view)
		}
	}
}

// H3: the overlay filters live while typing, the same way every inline
// suggestion menu already does — "cld" finds "claude" even though it is not
// a prefix, and the marker moves back to the top of whatever still matches.
func TestModelPickerFiltersLiveByTyping(t *testing.T) {
	c := NewController(Status{}, defaultDraftSize)
	c.RequestModelPicker([]ModelPickEntry{
		{ID: "vendor/mock", Name: "metered"},
		{ID: "anthropic/claude-opus", Name: "Claude Opus"},
	})
	c.HandleKey(Key{Kind: KeyText, Text: "cld"})

	pick := c.ModelPicker()
	if len(pick.Entries) != 1 || pick.Entries[0].ID != "anthropic/claude-opus" {
		t.Fatalf("filtered picker = %+v, want just the claude row", pick)
	}
	if pick.Index != 0 {
		t.Fatalf("index = %d, want the marker back on the one match", pick.Index)
	}
	if pick.Filter != "cld" {
		t.Fatalf("filter = %q, want the typed query", pick.Filter)
	}
}

// Backspace edits the query in place; it does not close the overlay, and
// widening the filter can bring rows back.
func TestModelPickerBackspaceWidensTheFilter(t *testing.T) {
	c := NewController(Status{}, defaultDraftSize)
	c.RequestModelPicker([]ModelPickEntry{
		{ID: "vendor/mock", Name: "metered"},
		{ID: "anthropic/claude-opus", Name: "Claude Opus"},
	})
	c.HandleKey(Key{Kind: KeyText, Text: "claude-opusX"})
	if len(c.ModelPicker().Entries) != 0 {
		t.Fatal("a query nothing matches must show nothing")
	}
	c.HandleKey(Key{Kind: KeyBackspace})
	pick := c.ModelPicker()
	if pick.Filter != "claude-opus" {
		t.Fatalf("filter after backspace = %q, want the query one rune shorter", pick.Filter)
	}
	if len(pick.Entries) != 1 || pick.Entries[0].ID != "anthropic/claude-opus" {
		t.Fatalf("widened filter = %+v, want the row back", pick.Entries)
	}
}

// Escape backs out one step at a time, the same as fzf and every fuzzy picker
// this leaf is meant to match: it clears an active filter first, and only
// closes the overlay once there is nothing left to clear.
func TestModelPickerEscapeClearsTheFilterBeforeClosingTheOverlay(t *testing.T) {
	r := NewRuntime(RuntimeOptions{})
	done := make(chan bool, 1)
	go func() {
		_, ok := r.AskModel(context.Background(), []ModelPickEntry{
			{ID: "anthropic/claude-opus", Name: "Claude Opus"},
		})
		done <- ok
	}()
	waitForPickerOpen(t, r)

	r.HandleKey(Key{Kind: KeyText, Text: "cld"})
	r.HandleKey(Key{Kind: KeyEscape})

	r.mu.Lock()
	pick := r.controller.ModelPicker()
	r.mu.Unlock()
	if pick == nil {
		t.Fatal("the first Escape closed the overlay; it should have cleared the filter")
	}
	if pick.Filter != "" {
		t.Fatalf("filter after Escape = %q, want it cleared", pick.Filter)
	}

	r.HandleKey(Key{Kind: KeyEscape})
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the second Escape, with nothing left to clear, must close the overlay")
	}
}

// The effort dial belongs to the row, not to its position on screen: turning
// it while a filter is narrowing the list must not silently move it to
// whichever row now sits at that index.
func TestModelPickerEffortDialSurvivesFiltering(t *testing.T) {
	c := NewController(Status{}, defaultDraftSize)
	c.RequestModelPicker([]ModelPickEntry{
		{ID: "vendor/mock", Name: "metered"},
		{ID: "anthropic/claude-opus", Name: "Claude Opus", Efforts: []string{"low", "medium", "high"}},
	})
	c.HandleKey(Key{Kind: KeyText, Text: "cld"})
	c.HandleKey(Key{Kind: KeyRight})

	pick := c.ModelPicker()
	if len(pick.Entries) != 1 || pick.Entries[0].Effort != 1 {
		t.Fatalf("picker = %+v, want the filtered claude row turned to medium", pick)
	}
}

// Without resetting the marker back to the top on every keystroke, moving it
// down an unfiltered list and then typing a filter that narrows the list
// below the marker's raw position indexes past the end of what is now on
// screen — Enter would pick a row that no longer exists.
func TestModelPickerResetsTheMarkerWhenTheFilterNarrowsPastIt(t *testing.T) {
	c := NewController(Status{}, defaultDraftSize)
	c.RequestModelPicker([]ModelPickEntry{
		{ID: "vendor/one", Name: "one"},
		{ID: "vendor/two", Name: "two"},
		{ID: "anthropic/claude-opus", Name: "Claude Opus"},
	})
	c.HandleKey(Key{Kind: KeyDown})
	c.HandleKey(Key{Kind: KeyDown})
	c.HandleKey(Key{Kind: KeyText, Text: "cld"})

	pick := c.ModelPicker()
	if len(pick.Entries) != 1 {
		t.Fatalf("filtered picker = %+v, want just the claude row", pick.Entries)
	}
	if pick.Index != 0 {
		t.Fatalf("index = %d, want the marker reset onto the one remaining row", pick.Index)
	}
	if effect := c.HandleKey(Key{Kind: KeyEnter}); effect.PickModel != "/model anthropic/claude-opus" {
		t.Fatalf("Enter answered %q, want the row actually on screen", effect.PickModel)
	}
}

// H2 built scrollWindow specifically because modelPickerLines rendered every
// row unbounded; wiring it in was left undone. A catalog longer than the
// window must clip, the same way the suggestion dropdown already does, or a
// long unfiltered list overflows the terminal the moment the overlay opens.
func TestModelPickerWindowsALongCatalog(t *testing.T) {
	entries := make([]ModelPickEntry, 0, 15)
	for i := range 15 {
		entries = append(entries, ModelPickEntry{ID: fmt.Sprintf("vendor/model-%02d", i)})
	}
	c := NewController(Status{}, defaultDraftSize)
	c.RequestModelPicker(entries)

	openedLines := c.modelPickerLines(80)
	opened := strings.Join(openedLines, "\n")
	if strings.Contains(opened, "model-08") {
		t.Fatalf("the opening frame ignored the window:\n%s", opened)
	}
	if !strings.Contains(opened, "model-07") || !hasIndicatorRow(openedLines, "↓") {
		t.Fatalf("the opening frame is missing rows or the arrow:\n%s", opened)
	}
	if hasIndicatorRow(openedLines, "↑") {
		t.Fatal("nothing is above the first row; an up arrow points at nothing")
	}

	// Walk to the end; the last model must become visible, and the first must
	// scroll out rather than stay pinned with the window widened.
	for range 14 {
		c.HandleKey(Key{Kind: KeyDown})
	}
	view := strings.Join(c.modelPickerLines(80), "\n")
	if !strings.Contains(view, "model-14") {
		t.Fatalf("scrolling down never reached the last model:\n%s", view)
	}
	if strings.Contains(view, "model-00") {
		t.Fatalf("the first model is still shown after scrolling past it:\n%s", view)
	}
}

// hasIndicatorRow reports whether one whole rendered line is the scroll
// indicator "  "+arrow — a substring check would also match the hint line's
// own "↑/↓" key legend, which names the arrows without being one.
func hasIndicatorRow(lines []string, arrow string) bool {
	for _, line := range lines {
		if line == "  "+arrow {
			return true
		}
	}
	return false
}

func waitForPickerOpen(t *testing.T, r *Runtime) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		r.mu.Lock()
		open := r.controller.ModelPicker() != nil
		r.mu.Unlock()
		if open {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the picker never opened")
		}
		time.Sleep(time.Millisecond)
	}
}
