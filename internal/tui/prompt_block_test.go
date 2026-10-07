package tui

import (
	"strings"
	"testing"
)

// The whole request the user sent is shown as theirs: every line of it, the
// wrapped ones included, in one block the eye can find. Before this only the
// first line was coloured, so a long or multi-line request looked like the
// model's own output from the second line down.
func TestTheWholeRequestIsStyledAsOneBlock(t *testing.T) {
	long := strings.Repeat("word ", 30)
	// A blank line inside the request is part of it: the owner's own message
	// had one, and it used to cut the block in two.
	rows := renderMarkdownStyled(promptEcho("first line "+long+"\n\nsecond line\nthird line")+"the model answers\n", 40)

	seen := 0
	for _, row := range rows {
		if strings.TrimSpace(row.text) == "" {
			continue
		}
		if strings.Contains(row.text, "the model answers") {
			if row.style == styleUser {
				t.Fatalf("the model's own output was styled as the user's: %q", row.text)
			}
			continue
		}
		if row.style != styleUser {
			t.Fatalf("row %q is styled %v, want the user block style", row.text, row.style)
		}
		seen++
	}
	if seen < 4 {
		t.Fatalf("only %d rows carried the request; a wrapped three-line request has more", seen)
	}
}

// The block ends where the request ends: an indented line that is not part
// of it keeps its own style.
func TestThePromptBlockEndsWithTheRequest(t *testing.T) {
	rows := renderMarkdownStyled(promptEcho("do the thing")+"\n  run so far: $1.10\n", 60)
	for _, row := range rows {
		if strings.Contains(row.text, "run so far") && row.style == styleUser {
			t.Fatalf("a line after the request was absorbed into it: %q", row.text)
		}
	}
}

func TestPromptsHaveSpacingAndFullRowShadingInEveryTheme(t *testing.T) {
	defer func() { _ = SetTheme("kolkrabbi"); SetPalette("256") }()
	if echo := promptEcho("first\n\nsecond"); !strings.HasPrefix(echo, "\n") || !strings.HasSuffix(echo, "\n\n") {
		t.Fatalf("prompt has no separation from adjacent logs: %q", echo)
	}
	for _, theme := range Themes() {
		_ = SetTheme(theme)
		for _, tier := range []string{"256", "16"} {
			SetPalette(tier)
			m := New(Status{})
			m.AppendTranscript(promptEcho("first\n\nsecond"))
			view := m.renderView(50, 14, -1)
			for _, row := range strings.Split(view, "\n") {
				if strings.Contains(row, "first") || strings.Contains(row, "second") {
					if !strings.Contains(row, activePalette[styleUser]) || activePalette[styleUser] == "" || visibleWidth(row) != 50 {
						t.Fatalf("theme %s tier %s does not shade the whole prompt row: %q", theme, tier, row)
					}
				}
			}
		}
	}
}

// A request that opens with a blank line (a pasted leading newline, or
// Shift+Enter first) is still one shaded block. Its marker row is "❯ " alone,
// which the renderer must not trim into a line it no longer recognises.
func TestAPromptOpeningWithABlankLineStaysOneBlock(t *testing.T) {
	for _, prompt := range []string{"\nfix the footer\nand the rows", "   \nfix the footer", "\n\nfix it"} {
		rows, _ := renderMarkdownStyledBlocks(promptEcho(prompt), 60)
		sawMarker := false
		for _, row := range rows {
			if strings.TrimSpace(row.text) == "" {
				continue
			}
			if row.style != styleUser {
				t.Fatalf("prompt %q: row %q is not shaded as the user's:\n%+v", prompt, row.text, rows)
			}
			sawMarker = sawMarker || strings.HasPrefix(row.text, promptMarker)
		}
		if !sawMarker {
			t.Fatalf("prompt %q lost its marker row:\n%+v", prompt, rows)
		}
	}
}
