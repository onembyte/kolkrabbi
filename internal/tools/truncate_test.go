package tools

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateNeverSplitsARune(t *testing.T) {
	// Any file with an accented name, a smart quote or an emoji hits this. A
	// byte-offset cut puts invalid UTF-8 into the tool result, which is then
	// sent to the provider and stored in the session.
	for _, filler := range []string{"é", "→", "🐙", "π"} {
		// The offset matters: maxOutput is 12000, which every one of these rune
		// widths divides exactly, so without a prefix the cut lands on a rune
		// boundary by accident and the test proves nothing.
		for _, offset := range []int{0, 1, 2, 3} {
			body := strings.Repeat("a", offset) + strings.Repeat(filler, maxOutput)
			got := truncate(body)
			if !utf8.ValidString(got) {
				t.Fatalf("truncating %q output at offset %d produced invalid UTF-8", filler, offset)
			}
		}
		body := strings.Repeat(filler, maxOutput)
		got := truncate(body)
		if !utf8.ValidString(got) {
			t.Fatalf("truncating %q output produced invalid UTF-8", filler)
		}
		if !strings.Contains(got, "truncated") {
			t.Fatalf("the cut was not announced for %q", filler)
		}
	}
}

func TestTruncateKeepsShortOutputExactly(t *testing.T) {
	body := "a short result\nwith two lines\n"
	if got := truncate(body); got != body {
		t.Fatalf("short output was altered: %q", got)
	}
}

func TestTruncatePrefersALineBoundary(t *testing.T) {
	// 81 bytes per line, which does not divide maxOutput, so the cut cannot
	// land on a line boundary by accident.
	line := strings.Repeat("x", 80) + "\n"
	body := strings.Repeat(line, (maxOutput/len(line))+20)

	got := truncate(body)

	kept, _, _ := strings.Cut(got, "\n... [truncated")
	// Output read by a model is easier to use whole-line; a half line at the
	// cut looks like the file itself is corrupt.
	if kept != "" && !strings.HasSuffix(kept, "\n") {
		t.Fatalf("cut mid-line: %q", kept[len(kept)-20:])
	}
}

func TestTruncateReportsWhatWasDropped(t *testing.T) {
	body := strings.Repeat("x", maxOutput+500)
	got := truncate(body)
	if !strings.Contains(got, "500 more") {
		t.Fatalf("the amount dropped was not reported: %q", got[len(got)-60:])
	}
}
