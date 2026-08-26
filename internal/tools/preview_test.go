package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// capturingGuard records what the confirmation would have shown, then allows.
func capturingGuard(seen *Request) Guard {
	return func(r Request) bool {
		*seen = r
		return true
	}
}

func previewFixture(t *testing.T) (string, *Request, Options) {
	t.Helper()
	root := t.TempDir()
	// Root is a containment boundary, not a resolution base: a relative path
	// still resolves against the process working directory. Without this the
	// tool writes into the repository and merely reports the write as being
	// outside the root.
	t.Chdir(root)
	seen := &Request{}
	return root, seen, Options{Root: root, Guard: capturingGuard(seen)}
}

func TestOverwritingAFileShowsWhatItReplaces(t *testing.T) {
	root, seen, opts := previewFixture(t)
	path := filepath.Join(root, "config.go")
	if err := os.WriteFile(path, []byte("package main\n\nconst Port = 8080\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Execute(context.Background(), "write_file",
		`{"path":"config.go","content":"package main\n\nconst Port = 9090\n","purpose":"change the port"}`, opts)
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// Showing only the new content means approving a description of the
	// change rather than the change.
	if !strings.Contains(seen.Detail, "-const Port = 8080") {
		t.Fatalf("detail did not show what is being destroyed:\n%s", seen.Detail)
	}
	if !strings.Contains(seen.Detail, "+const Port = 9090") {
		t.Fatalf("detail did not show the replacement:\n%s", seen.Detail)
	}
}

func TestCreatingAFileIsVisiblyNotAnOverwrite(t *testing.T) {
	_, seen, opts := previewFixture(t)

	_, err := Execute(context.Background(), "write_file",
		`{"path":"new.go","content":"package main\n","purpose":"add a file"}`, opts)
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}

	lowered := strings.ToLower(seen.Detail)
	if !strings.Contains(lowered, "new file") {
		t.Fatalf("a create did not say so:\n%s", seen.Detail)
	}
	if strings.Contains(seen.Detail, "\n-") {
		t.Fatalf("a create showed deletions:\n%s", seen.Detail)
	}
}

func TestAnEditShowsADiffNotTwoBlocks(t *testing.T) {
	root, seen, opts := previewFixture(t)
	path := filepath.Join(root, "a.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\nfour\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Execute(context.Background(), "edit_file",
		`{"path":"a.txt","old_str":"two","new_str":"TWO","purpose":"shout"}`, opts)
	if err != nil {
		t.Fatalf("edit failed: %v", err)
	}

	if !strings.Contains(seen.Detail, "-two") || !strings.Contains(seen.Detail, "+TWO") {
		t.Fatalf("detail = %q, want a diff", seen.Detail)
	}
	// Context is what tells the reader which "two" this is.
	if !strings.Contains(seen.Detail, " one") {
		t.Fatalf("detail = %q, want surrounding context", seen.Detail)
	}
	if !strings.Contains(seen.Detail, "@@") {
		t.Fatalf("detail = %q, want a located hunk", seen.Detail)
	}
}

func TestAHugePreviewIsCutInTheMiddle(t *testing.T) {
	root, seen, opts := previewFixture(t)
	path := filepath.Join(root, "big.txt")
	var before, after strings.Builder
	for i := range 400 {
		before.WriteString("old " + string(rune('a'+i%26)) + "\n")
		after.WriteString("new " + string(rune('a'+i%26)) + "\n")
	}
	if err := os.WriteFile(path, []byte(before.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Execute(context.Background(), "write_file",
		`{"path":"big.txt","content":`+quote(after.String())+`,"purpose":"rewrite"}`, opts)
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if !strings.Contains(seen.Detail, "not shown") {
		t.Fatalf("a huge diff was not truncated:\n%.400s", seen.Detail)
	}
	// The end of a change matters as much as its start.
	if !strings.Contains(seen.Detail, "+new") {
		t.Fatalf("truncation lost the new side:\n%.400s", seen.Detail)
	}
	if n := strings.Count(seen.Detail, "\n"); n > 60 {
		t.Fatalf("preview kept %d lines, too many to read at a prompt", n)
	}
}

func TestWritingIdenticalContentSaysNothingChanges(t *testing.T) {
	root, seen, opts := previewFixture(t)
	path := filepath.Join(root, "same.txt")
	if err := os.WriteFile(path, []byte("unchanged\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Execute(context.Background(), "write_file",
		`{"path":"same.txt","content":"unchanged\n","purpose":"no-op"}`, opts)
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// An empty diff shown as an empty prompt looks like a bug.
	if !strings.Contains(strings.ToLower(seen.Detail), "no change") {
		t.Fatalf("detail = %q, want it to say the file is unchanged", seen.Detail)
	}
}

func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\\':
			b.WriteString(`\\`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
