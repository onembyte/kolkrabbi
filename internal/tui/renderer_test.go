package tui

import (
	"bytes"
	"strings"
	"testing"
)

func TestRendererReplacesOnlyItsPreviousOwnedRows(t *testing.T) {
	var out bytes.Buffer
	renderer := NewRenderer(&out)
	if err := renderer.Render("assistant first\n╭─ kolk-code\n│ draft\n╰─"); err != nil {
		t.Fatal(err)
	}
	if err := renderer.Render("assistant second\n╭─ kolk-code\n│ draft\n╰─"); err != nil {
		t.Fatal(err)
	}

	want := "assistant first\r\n╭─ kolk-code\r\n│ draft\r\n╰─" +
		"\r\x1b[3A\x1b[J" +
		"assistant second\r\n╭─ kolk-code\r\n│ draft\r\n╰─"
	if got := out.String(); got != want {
		t.Fatalf("rendered bytes:\n got %q\nwant %q", got, want)
	}
}

func TestRendererUsesCarriageReturnLineFeedForRawTerminalRows(t *testing.T) {
	var out bytes.Buffer
	renderer := NewRenderer(&out)
	if err := renderer.Render("top\nmiddle\nbottom"); err != nil {
		t.Fatal(err)
	}

	if got, want := out.String(), "top\r\nmiddle\r\nbottom"; got != want {
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
	if err := renderer.Render("╭─ kolk-code\n│ draft\n╰─"); err != nil {
		t.Fatal(err)
	}
	if err := renderer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := renderer.Close(); err != nil {
		t.Fatal(err)
	}

	want := "\x1b[?2004h\x1b[?25l" +
		"╭─ kolk-code\r\n│ draft\r\n╰─" +
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
	if err := renderer.Render(row); err != nil {
		t.Fatal(err)
	}
	renderer.Resized(40)
	out.Reset()
	if err := renderer.Render("x"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\x1b[2A") {
		t.Fatalf("after narrowing to 40 the clear must move up 2 rows; got %q", out.String())
	}

	// Widening never increases the count: the frame still fits on one row.
	if err := renderer.Render(row); err != nil {
		t.Fatal(err)
	}
	renderer.Resized(200)
	out.Reset()
	if err := renderer.Render("x"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "A") {
		t.Fatalf("a wider terminal must not move the cursor up; got %q", out.String())
	}
}
