package tui

import (
	"strings"
	"testing"
)

// The whole point of a boundary is that cutting there changes nothing: the
// lines above render the same alone as they do in context. If that ever stops
// being true, committing a prefix to scrollback would show the reader
// something different from what was on screen a moment earlier.
func TestCuttingAtABoundaryChangesNothing(t *testing.T) {
	samples := []string{
		"plain line\nanother line\n",
		"# heading\nbody text\n\nmore body\n",
		"before\n```go\nfunc main() {}\n```\nafter\n",
		"a\n```diff\n+added\n-removed\n```\nb\n",
		"text\n```\nunclosed fence still streaming\n",
		"> quote\n- item one\n- item two\n\nparagraph\n",
		strings.Repeat("filler line\n", 40) + "```go\nx := 1\n```\ntail\n",
	}
	for _, width := range []int{20, 80} {
		for _, sample := range samples {
			whole, bounds := renderMarkdownBlocks(sample, width)
			source := strings.Split(sample, "\n")
			for _, b := range bounds {
				if b.source > len(source) {
					t.Fatalf("boundary source %d exceeds %d source lines", b.source, len(source))
				}
				if b.rendered > len(whole) {
					t.Fatalf("boundary rendered %d exceeds %d rendered lines", b.rendered, len(whole))
				}
				prefix := strings.Join(source[:b.source], "\n")
				if b.source > 0 {
					prefix += "\n"
				}
				got, _ := renderMarkdownBlocks(prefix, width)
				want := whole[:b.rendered]
				if len(got) != len(want) {
					t.Fatalf("width %d: prefix at source %d rendered %d lines, want %d\nsample: %q",
						width, b.source, len(got), len(want), sample)
				}
				for i := range got {
					if got[i] != want[i] {
						t.Fatalf("width %d: prefix at source %d line %d = %q, want %q",
							width, b.source, i, got[i], want[i])
					}
				}
			}
		}
	}
}

// An unclosed fence is still being streamed, so its rendering can still change.
// Cutting inside it would commit a half-drawn box that never gets its lid.
func TestAnUnclosedFenceIsNotABoundary(t *testing.T) {
	text := "intro\n```go\nfunc main() {\n"
	_, bounds := renderMarkdownBlocks(text, 40)
	source := strings.Split(text, "\n")
	for _, b := range bounds {
		if b.source > 1 {
			t.Errorf("boundary at source %d is inside an unclosed fence (source has %d lines)",
				b.source, len(source))
		}
	}
}

// Adopted from the V43.5 §7 item 2 verification (Y1): a boundary must leave
// both halves as they were, style included, because what is kept after the cut
// is the frame and, on exit, the scrollback. A work record cut after its
// heading rendered its remaining rows plain, so a record must not be cut.
func TestCuttingAtABoundaryChangesNeitherHalf(t *testing.T) {
	samples := []string{
		"plain line\nanother line\n",
		"# heading\nbody text\n\nmore body\n",
		"before\n```go\nfunc main() {}\n```\nafter\n",
		"> quote\n- item one\n- item two\n\nparagraph\n",
		"\n❯ a prompt\n  \n  and more of it\n\nthe answer\n",
		"\n• Failed make · kolk\n  └ × command failed\n  └ line one\n    line two\n    \n    line four\n  └ … 2 more lines\n\nafter\n",
		"• Explored · agent 1\n  └ Read a.go\n    ↳ find the entry point\n  └ Searched grep -rn x .\nprose after\n",
		"• an assistant bullet\n    deeper prose\n  indented prose\n",
		// Still streaming: the transcript ends inside a record.
		"• Ran ls · kolk\n  └ a.go\n    b.go",
	}
	for _, width := range []int{20, 80} {
		for _, sample := range samples {
			whole, bounds := renderMarkdownStyledBlocks(sample, width)
			source := strings.Split(sample, "\n")
			for _, b := range bounds {
				prefix := strings.Join(source[:b.source], "\n")
				if b.source > 0 {
					prefix += "\n"
				}
				suffix := strings.Join(source[b.source:], "\n")
				head, _ := renderMarkdownStyledBlocks(prefix, width)
				tail, _ := renderMarkdownStyledBlocks(suffix, width)
				got := append(append([]styledRow(nil), head...), tail...)
				if len(got) != len(whole) {
					t.Fatalf("width %d, cut at source %d: halves render %d rows, whole %d\nsample: %q",
						width, b.source, len(got), len(whole), sample)
				}
				for i := range got {
					if got[i].text != whole[i].text || got[i].style != whole[i].style {
						t.Fatalf("width %d, cut at source %d: row %d = %q (style %d), whole has %q (style %d)\nsample: %q",
							width, b.source, i, got[i].text, got[i].style, whole[i].text, whole[i].style, sample)
					}
				}
			}
		}
	}
}

// Adopted from the V43.5 §7 item 2 re-check (Y3): a record at the end of the
// transcript may still grow (grouped exploration appends rows under its
// heading later), so, like an unclosed fence, it is not a place to cut.
func TestAnOpenRecordIsNotABoundary(t *testing.T) {
	for _, text := range []string{
		"• Explored · agent 1\n  └ Read a.go\n",
		"• Ran ls · kolk\n  └ a.go\n    b.go",
	} {
		_, bounds := renderMarkdownStyledBlocks(text, 40)
		for _, b := range bounds {
			if b.source > 0 {
				t.Errorf("%q: boundary at source %d inside a record that may still grow", text, b.source)
			}
		}
	}
	// A record that something has followed is closed, and can be cut after.
	_, bounds := renderMarkdownStyledBlocks("• Explored · agent 1\n  └ Read a.go\n\nprose\n", 40)
	if len(bounds) < 3 {
		t.Errorf("a closed record offered %d boundaries, want the ones after it", len(bounds))
	}
}
