package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func readFile(t *testing.T, args map[string]any) (string, error) {
	t.Helper()
	encoded, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return Execute(context.Background(), "read_file", string(encoded), allowAll())
}

func writeLines(t *testing.T, count int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "big.txt")
	var b strings.Builder
	for i := 1; i <= count; i++ {
		b.WriteString("line " + strconv.Itoa(i) + "\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadFileReturnsARequestedRange(t *testing.T) {
	path := writeLines(t, 200)

	out, err := readFile(t, map[string]any{"path": path, "start_line": 10, "end_line": 12})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"line 10", "line 11", "line 12"} {
		if !strings.Contains(out, want) {
			t.Fatalf("range is missing %q: %q", want, out)
		}
	}
	for _, unwanted := range []string{"line 9\n", "line 13"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("range leaked %q: %q", unwanted, out)
		}
	}
	// Line numbers stay absolute, or an edit built from them lands in the
	// wrong place.
	if !strings.Contains(out, "   10\t") {
		t.Fatalf("range renumbered the file: %q", out)
	}
}

func TestARangeBeyondTheFileSaysSoRatherThanReturningNothing(t *testing.T) {
	path := writeLines(t, 5)

	out, err := readFile(t, map[string]any{"path": path, "start_line": 90, "end_line": 100})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "5 lines") {
		t.Fatalf("out = %q, want the real length so the model can correct itself", out)
	}
}

func TestAnUnrangedReadOfALargeFileSaysHowToGetTheRest(t *testing.T) {
	path := writeLines(t, 20000)

	out, err := readFile(t, map[string]any{"path": path})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "line 1\n") {
		t.Fatalf("the head is missing: %q", out[:120])
	}
	// Truncation that does not say how to continue leaves the model guessing,
	// and it usually guesses "run grep in bash".
	if !strings.Contains(out, "start_line") {
		t.Fatalf("out does not tell the model how to read the rest: %q", out[len(out)-200:])
	}
	if !strings.Contains(out, "20000") {
		t.Fatalf("out does not state the file's length: %q", out[len(out)-200:])
	}
}

func TestASmallFileIsUnchanged(t *testing.T) {
	path := writeLines(t, 3)

	out, err := readFile(t, map[string]any{"path": path})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "start_line") || strings.Contains(out, "truncated") {
		t.Fatalf("a small file was decorated: %q", out)
	}
}

func TestBinaryFilesAreDescribedNotSent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.bin")
	body := append([]byte("ELF\x00\x01\x02binary"), make([]byte, 4096)...)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := readFile(t, map[string]any{"path": path})
	if err != nil {
		t.Fatal(err)
	}
	// Sending a binary wastes the window and can carry bytes no provider can
	// represent.
	if strings.Contains(out, "\x00") {
		t.Fatalf("binary content reached the conversation: %q", out[:60])
	}
	if !strings.Contains(out, "binary") {
		t.Fatalf("out = %q, want it named as binary", out)
	}
	if !strings.Contains(out, strconv.Itoa(len(body))) {
		t.Fatalf("out = %q, want the size so the model knows what it skipped", out)
	}
}

func TestAFileOfTextWithHighBytesIsStillText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(path, []byte("# π approximates 3.14159 — été 🐙\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := readFile(t, map[string]any{"path": path})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "binary") {
		t.Fatalf("a UTF-8 file was called binary: %q", out)
	}
	if !strings.Contains(out, "🐙") {
		t.Fatalf("content was lost: %q", out)
	}
}
