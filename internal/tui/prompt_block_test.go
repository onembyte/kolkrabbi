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
