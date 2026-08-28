package tui

import (
	"bytes"
	"strings"
	"testing"
)

func TestRendererReplacesOnlyItsPreviousOwnedRows(t *testing.T) {
	var out bytes.Buffer
	renderer := NewRenderer(&out)
	if err := renderer.Render(nil, "assistant first\n╭─ kolk-code\n│ draft\n╰─"); err != nil {
		t.Fatal(err)
	}
	if err := renderer.Render(nil, "assistant second\n╭─ kolk-code\n│ draft\n╰─"); err != nil {
		t.Fatal(err)
	}

	want := "\rassistant first\x1b[K\r\n╭─ kolk-code\x1b[K\r\n│ draft\x1b[K\r\n╰─\x1b[K" +
		"\r\x1b[3A" +
		"assistant second\x1b[K\r\n╭─ kolk-code\x1b[K\r\n│ draft\x1b[K\r\n╰─\x1b[K"
	if got := out.String(); got != want {
		t.Fatalf("rendered bytes:\n got %q\nwant %q", got, want)
	}
}

// The reason for the shape above: a repaint must never blank the region before
// it draws. Erase-then-draw let the terminal display the empty state in
// between, and a window drag — which fires a repaint per size change — turned
// that into a visible flicker of the whole composer.
func TestRendererOverwritesInPlaceWithoutBlankingFirst(t *testing.T) {
	var out bytes.Buffer
	renderer := NewRenderer(&out)
	if err := renderer.Render(nil, "one\ntwo\nthree"); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := renderer.Render(nil, "ONE\nTWO\nTHREE"); err != nil {
		t.Fatal(err)
	}
	frame := out.String()

	// Whatever precedes the first character of content may only move the
	// cursor. An erase there is the flicker.
	head := frame[:strings.Index(frame, "ONE")]
	if strings.Contains(head, "\x1b[J") || strings.Contains(head, "\x1b[2J") {
		t.Fatalf("region was erased before it was redrawn: %q", head)
	}
	// Every row clears its own tail instead, so a shorter line cannot leave
	// the previous one's characters behind.
	if got := strings.Count(frame, "\x1b[K"); got != 3 {
		t.Fatalf("erase-to-end-of-line count = %d, want one per row", got)
	}
	// One syscall per frame: a partially written frame is a partially drawn one.
	var writes countingWriter
	second := NewRenderer(&writes)
	if err := second.Render(nil, "a\nb\nc"); err != nil {
		t.Fatal(err)
	}
	if writes.n != 1 {
		t.Fatalf("frame took %d writes, want 1", writes.n)
	}
}

// A frame shorter than the last still has to remove the rows it no longer uses.
func TestRendererErasesTheTailWhenAFrameShrinks(t *testing.T) {
	var out bytes.Buffer
	renderer := NewRenderer(&out)
	if err := renderer.Render(nil, "one\ntwo\nthree\nfour"); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := renderer.Render(nil, "one\ntwo"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out.String(), "\x1b[J") {
		t.Fatalf("a shrinking frame must erase what is left below it: %q", out.String())
	}
}

type countingWriter struct{ n int }

func (w *countingWriter) Write(p []byte) (int, error) { w.n++; return len(p), nil }

func TestRendererUsesCarriageReturnLineFeedForRawTerminalRows(t *testing.T) {
	var out bytes.Buffer
	renderer := NewRenderer(&out)
	if err := renderer.Render(nil, "top\nmiddle\nbottom"); err != nil {
		t.Fatal(err)
	}

	if got, want := out.String(), "\rtop\x1b[K\r\nmiddle\x1b[K\r\nbottom\x1b[K"; got != want {
		t.Fatalf("raw-terminal frame = %q, want %q", got, want)
	}
}

func TestRendererStartAndCloseOwnBracketedPasteAndCursorState(t *testing.T) {
	var out bytes.Buffer
	renderer := NewRenderer(&out)
	if err := renderer.Start(); err != nil {
		t.Fatal(err)
	}
	if err := renderer.Start(); err != nil {
		t.Fatal(err)
	}
	if err := renderer.Render(nil, "╭─ kolk-code\n│ draft\n╰─"); err != nil {
		t.Fatal(err)
	}
	if err := renderer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := renderer.Close(); err != nil {
		t.Fatal(err)
	}

	want := "\x1b[?2004h\x1b[?25l" +
		"\r╭─ kolk-code\x1b[K\r\n│ draft\x1b[K\r\n╰─\x1b[K" +
		"\r\x1b[2A\x1b[J\x1b[?25h\x1b[?2004l"
	if got := out.String(); got != want {
		t.Fatalf("lifecycle bytes:\n got %q\nwant %q", got, want)
	}
}

func TestRendererClearsThePhysicalRowsAFrameOccupiesAfterANarrowing(t *testing.T) {
	var out bytes.Buffer
	renderer := NewRenderer(&out)
	if err := renderer.Start(); err != nil {
		t.Fatal(err)
	}
	// One logical row of 100 cells, some of them styled: at width 100 it is one
	// physical row, at width 40 the terminal re-flows it onto three.
	row := "\x1b[35m" + strings.Repeat("─", 60) + "\x1b[0m" + strings.Repeat("─", 40)
	if err := renderer.Render(nil, row); err != nil {
		t.Fatal(err)
	}
	renderer.Resized(40)
	out.Reset()
	if err := renderer.Render(nil, "x"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\x1b[2A") {
		t.Fatalf("after narrowing to 40 the clear must move up 2 rows; got %q", out.String())
	}

	// Widening never increases the count: the frame still fits on one row.
	if err := renderer.Render(nil, row); err != nil {
		t.Fatal(err)
	}
	renderer.Resized(200)
	out.Reset()
	if err := renderer.Render(nil, "x"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "A") {
		t.Fatalf("a wider terminal must not move the cursor up; got %q", out.String())
	}
}
