package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// allowAll is the default policy: a nil guard permits everything, which is
// what a caller with no policy expects.
func allowAll() Options { return Options{} }

func denyAll() Options { return Options{Guard: func(Request) bool { return false }} }

func TestWriteReadEditFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "hello.txt")

	// write_file should create parent dirs
	_, err := Execute(context.Background(), "write_file", `{"path":"`+jsonEsc(p)+`","content":"line one\nline two\n"}`, allowAll())
	if err != nil {
		t.Fatalf("write_file: %v", err)
	}

	out, err := Execute(context.Background(), "read_file", `{"path":"`+jsonEsc(p)+`"}`, allowAll())
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	if !strings.Contains(out, "line one") || !strings.Contains(out, "line two") {
		t.Errorf("read_file output missing content: %q", out)
	}

	// edit_file: unique replace should succeed
	_, err = Execute(context.Background(), "edit_file", `{"path":"`+jsonEsc(p)+`","old_str":"line one","new_str":"LINE ONE"}`, allowAll())
	if err != nil {
		t.Fatalf("edit_file: %v", err)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "LINE ONE") {
		t.Errorf("edit did not apply: %q", string(b))
	}

	// edit_file: non-existent old_str should error
	_, err = Execute(context.Background(), "edit_file", `{"path":"`+jsonEsc(p)+`","old_str":"nope","new_str":"x"}`, allowAll())
	if err == nil {
		t.Error("expected error for missing old_str, got nil")
	}

	// write_file should refuse when confirm denies
	_, err = Execute(context.Background(), "write_file", `{"path":"`+jsonEsc(p)+`","content":"nope"}`, denyAll())
	if err == nil {
		t.Error("expected error when confirm denies write, got nil")
	}
}

func TestEditFile_NonUniqueMatch(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "dup.txt")
	os.WriteFile(p, []byte("foo\nfoo\n"), 0o644)

	_, err := Execute(context.Background(), "edit_file", `{"path":"`+jsonEsc(p)+`","old_str":"foo","new_str":"bar"}`, allowAll())
	if err == nil || !strings.Contains(err.Error(), "not unique") {
		t.Errorf("expected 'not unique' error, got: %v", err)
	}
}

func TestListDir(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644)
	os.Mkdir(filepath.Join(dir, "sub"), 0o755)

	out, err := Execute(context.Background(), "list_dir", `{"path":"`+jsonEsc(dir)+`"}`, allowAll())
	if err != nil {
		t.Fatalf("list_dir: %v", err)
	}
	if !strings.Contains(out, "a.txt") || !strings.Contains(out, "sub") {
		t.Errorf("list_dir missing entries: %q", out)
	}
}

func TestBash(t *testing.T) {
	out, err := Execute(context.Background(), "bash", `{"command":"echo hello_kolk","description":"say hello"}`, allowAll())
	if err != nil {
		t.Fatalf("bash: %v", err)
	}
	if !strings.Contains(out, "hello_kolk") {
		t.Errorf("bash output = %q, want it to contain hello_kolk", out)
	}

	_, err = Execute(context.Background(), "bash", `{"command":"echo nope","description":"nope"}`, denyAll())
	if err == nil {
		t.Error("expected error when confirm denies bash, got nil")
	}
}

func jsonEsc(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return r.Replace(s)
}

func TestPreWriteHook(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "guarded.txt")
	os.WriteFile(p, []byte("original"), 0o644)

	// hook receives the right tool+path, and runs before the mutation
	var gotTool, gotPath string
	pre := func(tool, path string) error {
		gotTool, gotPath = tool, path
		b, _ := os.ReadFile(p)
		if string(b) != "original" {
			t.Error("pre hook ran after the file was already modified")
		}
		return nil
	}
	_, err := Execute(context.Background(), "write_file", `{"path":"`+jsonEsc(p)+`","content":"changed"}`, Options{PreWrite: pre})
	if err != nil {
		t.Fatalf("write_file with pre hook: %v", err)
	}
	if gotTool != "write_file" || gotPath != p {
		t.Errorf("pre hook got (%q,%q), want (write_file,%q)", gotTool, gotPath, p)
	}

	// a failing hook must abort the operation and leave the file untouched
	failing := func(tool, path string) error { return os.ErrPermission }
	_, err = Execute(context.Background(), "edit_file", `{"path":"`+jsonEsc(p)+`","old_str":"changed","new_str":"nope"}`, Options{PreWrite: failing})
	if err == nil {
		t.Fatal("expected error when pre hook fails, got nil")
	}
	b, _ := os.ReadFile(p)
	if string(b) != "changed" {
		t.Errorf("file was modified despite failing pre hook: %q", string(b))
	}
}
