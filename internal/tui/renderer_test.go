package tui

import (
	"bytes"
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

	want := "assistant first\n╭─ kolk-code\n│ draft\n╰─" +
		"\r\x1b[3A\x1b[J" +
		"assistant second\n╭─ kolk-code\n│ draft\n╰─"
	if got := out.String(); got != want {
		t.Fatalf("rendered bytes:\n got %q\nwant %q", got, want)
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
		"╭─ kolk-code\n│ draft\n╰─" +
		"\r\x1b[2A\x1b[J\x1b[?25h\x1b[?2004l"
	if got := out.String(); got != want {
		t.Fatalf("lifecycle bytes:\n got %q\nwant %q", got, want)
	}
}
