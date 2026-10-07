package tui

import (
	"strings"
	"testing"
)

func renderTranscript(t *testing.T, chunk string, width int) []string {
	t.Helper()
	m := New(Status{Mode: "code", Lifecycle: "ready"})
	m.AppendTranscript(chunk)
	// Unbounded height: a bounded frame is padded to fill the terminal so the
	// composer sits on its last row, and these tests are about what the
	// markdown renderer produces, not where the layout puts it.
	return strings.Split(m.View(width, 0), "\n")
}

func TestMarkdownRendersHeadingsListsAndQuotes(t *testing.T) {
	lines := renderTranscript(t, "## Plan\n- one\n* two\n1. three\n> quoted note\ntext after\n", 40)

	want := []string{
		"Plan",
		"",
		"  · one",
		"  · two",
		"  1. three",
		"  │ quoted note",
		"text after",
	}
	if len(lines) < len(want)+4 || lines[len(lines)-2] != strings.Repeat("─", 40) {
		t.Fatalf("composer missing from view:\n%s", strings.Join(lines, "\n"))
	}
	got := lines[:len(want)]
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d = %q, want %q\nfull view:\n%s", i, got[i], want[i], strings.Join(lines, "\n"))
		}
	}
}

func TestMarkdownRendersFencedCodeBlocksInVisualTokens(t *testing.T) {
	lines := renderTranscript(t, "before\n```go\nfmt.Println(\"hi\")\nx < 1\n```\nafter\n", 40)

	want := []string{
		"before",
		"╭─ go",
		"│ fmt.Println(\"hi\")",
		"│ x < 1",
		"╰─",
		"after",
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("line %d = %q, want %q\nfull view:\n%s", i, lines[i], want[i], strings.Join(lines, "\n"))
		}
	}
}

func TestMarkdownCodeFencesRequireClosingFenceOnTheirOwnLine(t *testing.T) {
	lines := renderTranscript(t, "```go\nvalue := 1\n\nplain text\n", 40)

	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "│ value := 1") {
		t.Fatalf("fence body not rendered as code:\n%s", joined)
	}
	if strings.Contains(joined, "│ plain text") {
		t.Fatalf("prose swallowed by unterminated fence:\n%s", joined)
	}
}

func TestDiffBlocksRenderAddRemoveAndContextLines(t *testing.T) {
	lines := renderTranscript(t, "patch:\n```diff\n context\n-old\n+new\n```\n", 40)

	want := []string{
		"patch:",
		"╭─ diff",
		"│    context",
		"│ - old",
		"│ + new",
		"╰─",
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("line %d = %q, want %q\nfull view:\n%s", i, lines[i], want[i], strings.Join(lines, "\n"))
		}
	}
}

func TestMarkdownRenderingIsDeterministicAcrossRepeatedViews(t *testing.T) {
	chunk := "# Title\n\n```py\nfor i in range(3):\n    print(i)\n```\n- a\n- b\n"
	m := New(Status{Mode: "chat", Lifecycle: "ready"})
	m.AppendTranscript(chunk)
	first := m.View(80, 30)
	for range 9 {
		if again := m.View(80, 30); again != first {
			t.Fatalf("repeated views diverged:\nfirst:\n%s\nagain:\n%s", first, again)
		}
	}
}

func TestMarkdownNarrowWidthKeepsCellAlignmentAndNeverMutatesSource(t *testing.T) {
	chunk := "## 標題\n- emoji 🐙 item\n+diff line\n```diff\n+added 中文\n```\n"
	m := New(Status{Mode: "code", Lifecycle: "ready"})
	m.AppendTranscript(chunk)
	snapshot := m.Snapshot().Transcript

	for _, width := range []int{4, 5, 11, 20} {
		for _, line := range strings.Split(m.View(width, 24), "\n") {
			if cells := cellWidth(line); cells > width {
				t.Fatalf("width %d produced %d-cell line %q", width, cells, line)
			}
		}
	}
	if m.Snapshot().Transcript != snapshot {
		t.Fatal("rendering mutated the stored transcript")
	}
}

func TestMarkdownStreamingSplitsRenderWithoutDuplicatedOutput(t *testing.T) {
	controller := NewController(Status{Mode: "code", Lifecycle: "ready"}, 64)
	controller.AppendTranscript("```diff\n+one\n")
	controller.AppendTranscript("+two\n```\ndone\n")

	view := controller.View(60, 24)
	for _, token := range []string{"+ one", "+ two", "done"} {
		if strings.Count(view, token) != 1 {
			t.Fatalf("token %q appears %d times in:\n%s", token, strings.Count(view, token), view)
		}
	}
}

func TestMarkdownControlSequencesAreStrippedBeforeStructuralParsing(t *testing.T) {
	lines := renderTranscript(t, "\x1b[31m## Heading\x1b[0m\n\x1b]8;;http://x\x07- link\x1b]8;;\x07\n", 40)

	if lines[0] != "Heading" {
		t.Fatalf("heading kept ANSI bytes: %q", lines[0])
	}
	if lines[1] != "" || lines[2] != "  · link" {
		t.Fatalf("list row kept OSC bytes: %q / %q", lines[1], lines[2])
	}
}

// Adopted from the V43.5 §7 item 3 exercise (Z2): a wrapped numbered item, a
// plan task's route included, and kolk's own ◆ notices continue under their
// text, as bullets already do, not at column 0 where they read as new lines.
func TestNumberedItemsAndNoticesHangUnderTheirText(t *testing.T) {
	for _, c := range []struct {
		line, hang string
	}{
		{"  1. T1 mechanical  [boilerplate · trivial · small · low]", "     "},
		{"1. an assistant's numbered item that wraps across rows", "     "},
		{"12. a twelfth numbered item that also wraps across rows", "      "},
		{"◆ paused: mid hit its subscription allowance; reset at 01:34; /resume to retry", "  "},
		{"  ◆ T2 routine could not start on mid; falling back to sel", "    "},
		// A number that merely starts a line is not an item: no hang.
		{"  3.14 is only a number that starts this long line of prose", ""},
		{"  . and a bare dot is no item either, on this long line", ""},
	} {
		rows, _ := renderMarkdownStyledBlocks(c.line+"\n", 30)
		if len(rows) < 2 {
			t.Fatalf("%q did not wrap at 30 columns", c.line)
		}
		for _, row := range rows[1:] {
			if row.text == "" {
				continue
			}
			if !strings.HasPrefix(row.text, c.hang) || strings.HasPrefix(row.text, c.hang+" ") || cellWidth(row.text) > 30 {
				t.Errorf("%q: continuation %q, want it under the text at %d columns", c.line, row.text, len(c.hang))
			}
		}
	}
}

// Adopted from the V43.5 §7 item 3 verification (Z3): a hang that leaves no
// room for the text clipped every row to "…", so the line said nothing and
// took dozens of rows. Too narrow to hang, a line wraps like prose instead.
func TestAHangWiderThanTheRowGivesWayToTheText(t *testing.T) {
	lines := map[string]string{
		"20250514. install the runtime before the first request": "20250514.",
		"1. install the runtime":                                 "1.",
		"◆ paused: the model hit its allowance":                  "◆",
		"  ◆ T2 could not start on mid; falling back":            "◆",
		"- a bullet item with some text":                         "·",
		"    ↳ find the entry point":                             "↳",
	}
	for width := 4; width <= 12; width++ {
		for line, marker := range lines {
			rows, _ := renderMarkdownStyledBlocks(line+"\n", width)
			// Giving way drops the hang, never the marker that says what the
			// line is (a marker wider than the row may itself be broken).
			var text strings.Builder
			for _, row := range rows {
				text.WriteString(strings.TrimSpace(row.text))
			}
			if !strings.Contains(text.String(), marker) {
				t.Errorf("width %d, %q: the rows %q lost the marker %q", width, line, text.String(), marker)
			}
			// A column one character wide is as unreadable as a column of "…".
			if len(rows) > cellWidth(line)/2 {
				t.Errorf("width %d, %q: %d rows for %d cells", width, line, len(rows), cellWidth(line))
			}
			for _, row := range rows {
				if strings.TrimSpace(row.text) == "…" || cellWidth(row.text) > width {
					t.Errorf("width %d, %q: row %q lost its text or overflowed", width, line, row.text)
					break
				}
			}
		}
	}
}
