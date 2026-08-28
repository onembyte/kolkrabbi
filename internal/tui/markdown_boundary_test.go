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
