package atomicfile

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWriteBeneathCreatesDirectoriesAndHonoursTheMode(t *testing.T) {
	root := realTemp(t)
	p := filepath.Join(root, "sub", "deeper", "a.txt")
	if err := WriteBeneath(root, p, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteBeneath(root, p, []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(p)
	if err != nil || string(got) != "two" {
		t.Fatalf("content = %q, %v", got, err)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(p)
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("mode = %o, want 600", info.Mode().Perm())
		}
	}
	if left, _ := filepath.Glob(filepath.Join(root, "sub", "deeper", ".*.tmp")); len(left) != 0 {
		t.Fatalf("temporary debris left behind: %v", left)
	}
}

func TestWriteBeneathRefusesAPathOutsideTheRoot(t *testing.T) {
	root := realTemp(t)
	for _, p := range []string{filepath.Join(root, "..", "x"), filepath.Dir(root), root, "relative.txt"} {
		if err := WriteBeneath(root, p, []byte("x"), 0o644); err == nil {
			t.Errorf("WriteBeneath(%s) succeeded outside %s", p, root)
		}
	}
}

// A link where the file should be is replaced by the file. The link's target
// -- the thing an attacker wanted written -- is never opened.
func TestWriteBeneathReplacesALinkAtThePathWithoutFollowingIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics")
	}
	root, outside := realTemp(t), realTemp(t)
	victim := filepath.Join(outside, "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "a.txt")
	if err := os.Symlink(victim, p); err != nil {
		t.Fatal(err)
	}
	if err := WriteBeneath(root, p, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(victim); string(got) != "keep" {
		t.Fatalf("the link was followed: victim = %q", got)
	}
	info, err := os.Lstat(p)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("the path is not a regular file after the write: %v %v", info, err)
	}
	if got, _ := os.ReadFile(p); string(got) != "new" {
		t.Fatalf("content = %q", got)
	}
}

// A link where a directory should be is the escape: refused, nothing written.
func TestWriteBeneathRefusesALinkedDirectoryComponent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics")
	}
	root, outside := realTemp(t), realTemp(t)
	if err := os.Symlink(outside, filepath.Join(root, "sub")); err != nil {
		t.Fatal(err)
	}
	err := WriteBeneath(root, filepath.Join(root, "sub", "a.txt"), []byte("x"), 0o644)
	if err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("err = %v, want a refusal naming the link", err)
	}
	if _, statErr := os.Lstat(filepath.Join(outside, "a.txt")); statErr == nil {
		t.Fatal("a file was written through the linked directory")
	}
}

// realTemp resolves the temp dir's own links (macOS puts /var behind /private)
// so the root handed to WriteBeneath is the real directory it demands.
func realTemp(t *testing.T) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return real
}
