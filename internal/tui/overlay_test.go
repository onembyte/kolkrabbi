package tui

import "testing"

// filterBox exists so a filterable overlay's query line can grow and shrink
// without pulling in composer's Editor: the composer needs cursor movement,
// multiline input and history, and a filterable overlay's arrow keys are
// claimed for row navigation (and, in /model's case, the effort dial)
// instead — there is no cursor position left for Left/Right to move.
func TestFilterBoxAppendsAndBackspaces(t *testing.T) {
	var box filterBox
	box.insert("cl")
	box.insert("d")
	if got := box.String(); got != "cld" {
		t.Fatalf("filter = %q, want %q", got, "cld")
	}
	if !box.backspace() {
		t.Fatal("backspace on a non-empty box must report it changed something")
	}
	if got := box.String(); got != "cl" {
		t.Fatalf("filter after backspace = %q, want %q", got, "cl")
	}
}

// An empty box has nothing to remove, and the caller (deciding whether to
// clear the filter or close the overlay on Escape) needs to know that.
func TestFilterBoxBackspaceOnEmptyReportsNoChange(t *testing.T) {
	var box filterBox
	if box.backspace() {
		t.Fatal("backspace on an empty box reported a change")
	}
}

// scrollWindow is the suggestion dropdown's own least-scroll rule, extracted
// so a second overlay does not carry a second copy of the same three
// comparisons: scroll the least amount that still keeps the selection
// visible inside a window of the given size.
func TestScrollWindowKeepsSelectionVisibleWithMinimalMovement(t *testing.T) {
	// Selection is already inside the window: no scroll.
	if got := scrollWindow(3, 0, 8); got != 0 {
		t.Fatalf("top = %d, want 0 (selection already visible)", got)
	}
	// Selection has moved above the window: scroll up to meet it exactly.
	if got := scrollWindow(2, 5, 8); got != 2 {
		t.Fatalf("top = %d, want 2 (scroll up to the selection)", got)
	}
	// Selection has moved past the window's far edge: scroll down the least
	// amount that puts it on the last visible row, not the first.
	if got := scrollWindow(15, 0, 8); got != 8 {
		t.Fatalf("top = %d, want 8 (scroll down just enough)", got)
	}
}

// The window can never scroll past the top of the list, however the
// selection got there.
func TestScrollWindowNeverGoesNegative(t *testing.T) {
	if got := scrollWindow(0, 0, 8); got != 0 {
		t.Fatalf("top = %d, want 0", got)
	}
}
