package diff

import (
	"strings"
	"testing"
)

func TestNoChangeIsNoDiff(t *testing.T) {
	if got := Unified("a\nb\nc\n", "a\nb\nc\n", 3); got != "" {
		t.Fatalf("got %q, want nothing", got)
	}
}

func TestAChangedLineShowsBothSides(t *testing.T) {
	got := Unified("a\nb\nc\n", "a\nB\nc\n", 3)

	if !strings.Contains(got, "-b") || !strings.Contains(got, "+B") {
		t.Fatalf("diff = %q, want the old and new line", got)
	}
	// Context is what makes a diff readable rather than a pair of fragments.
	if !strings.Contains(got, " a") || !strings.Contains(got, " c") {
		t.Fatalf("diff = %q, want surrounding context", got)
	}
}

func TestInsertionsAndDeletionsAreDistinguishable(t *testing.T) {
	// Counted on body lines only: a hunk header contains both a - and a +.
	inserted := Unified("a\nc\n", "a\nb\nc\n", 3)
	if !strings.Contains(inserted, "+b") || bodyLines(inserted, "-") != 0 {
		t.Fatalf("insertion = %q", inserted)
	}
	deleted := Unified("a\nb\nc\n", "a\nc\n", 3)
	if !strings.Contains(deleted, "-b") || bodyLines(deleted, "+") != 0 {
		t.Fatalf("deletion = %q", deleted)
	}
}

func TestDistantChangesBecomeSeparateHunks(t *testing.T) {
	before := strings.Repeat("x\n", 40) + "old\n" + strings.Repeat("y\n", 40) + "gone\n"
	after := strings.Repeat("x\n", 40) + "new\n" + strings.Repeat("y\n", 40) + "kept\n"

	got := Unified(before, after, 2)

	// Forty unchanged lines between two edits is not context, it is noise.
	if n := bodyLines(got, "@@"); n != 2 {
		t.Fatalf("got %d hunks, want 2:\n%s", n, got)
	}
	if strings.Count(got, "\n") > 20 {
		t.Fatalf("hunks did not limit context:\n%s", got)
	}
}

func TestAHunkHeaderSaysWhereInTheFile(t *testing.T) {
	before := "a\nb\nc\nd\ne\nf\ng\nh\n"
	after := "a\nb\nc\nd\nE\nf\ng\nh\n"

	got := Unified(before, after, 1)

	// A diff that does not say where it is makes the reader search for it.
	if !strings.Contains(got, "@@ -4,3 +4,3 @@") {
		t.Fatalf("diff = %q, want a located hunk header", got)
	}
}

func TestAFileWithNoTrailingNewlineIsNotMisreported(t *testing.T) {
	got := Unified("a\nb", "a\nB", 3)
	if !strings.Contains(got, "-b") || !strings.Contains(got, "+B") {
		t.Fatalf("diff = %q", got)
	}
	// The absent newline must not appear as an extra empty line on either side.
	if strings.Contains(got, "-\n") || strings.Contains(got, "+\n") {
		t.Fatalf("diff = %q, want no phantom empty line", got)
	}
}

func TestAHugeRewriteStillProducesSomething(t *testing.T) {
	// Two files with nothing in common are the worst case for a line diff, and
	// the one where an unbounded algorithm stops being a preview and becomes a
	// hang in front of a person waiting to answer a prompt.
	before := ""
	after := ""
	for i := range 4000 {
		before += "old line " + itoa(i) + "\n"
		after += "new line " + itoa(i) + "\n"
	}
	got := Unified(before, after, 3)
	if got == "" {
		t.Fatal("a total rewrite produced no diff at all")
	}
	if !strings.Contains(got, "-") || !strings.Contains(got, "+") {
		t.Fatalf("diff lost one side: %.200q", got)
	}
}

func TestTruncationCutsTheMiddleAndSaysHowMuch(t *testing.T) {
	var b strings.Builder
	for i := range 100 {
		b.WriteString("+line " + itoa(i) + "\n")
	}

	got := Truncate(b.String(), 20)

	// The last hunk matters as much as the first. A preview that always drops
	// the tail teaches people the tail does not matter.
	if !strings.Contains(got, "+line 0") {
		t.Fatalf("lost the head:\n%s", got)
	}
	if !strings.Contains(got, "+line 99") {
		t.Fatalf("lost the tail:\n%s", got)
	}
	if !strings.Contains(got, "not shown") {
		t.Fatalf("hid lines without saying so:\n%s", got)
	}
	if n := strings.Count(got, "\n"); n > 22 {
		t.Fatalf("kept %d lines, want about 20:\n%s", n, got)
	}
}

func TestShortDiffsAreNotTruncated(t *testing.T) {
	short := "-a\n+b\n"
	if got := Truncate(short, 20); got != short {
		t.Fatalf("got %q, want it untouched", got)
	}
}

// bodyLines counts diff lines starting with a marker, ignoring that a hunk
// header happens to contain every marker there is.
func bodyLines(diff, marker string) int {
	n := 0
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "@@") && marker != "@@" {
			continue
		}
		if strings.HasPrefix(line, marker) {
			n++
		}
	}
	return n
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var digits []byte
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}
