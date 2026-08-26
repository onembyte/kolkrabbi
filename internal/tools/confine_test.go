package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveInsideTheRoot(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	for _, path := range []string{"main.go", "./pkg/thing.go", "pkg/../main.go", filepath.Join(root, "deep", "file.txt")} {
		resolved, outside, err := resolvePath(root, path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if outside {
			t.Fatalf("%s resolved to %s, reported outside the root %s", path, resolved, root)
		}
		if !filepath.IsAbs(resolved) {
			t.Fatalf("%s resolved to a relative path %q", path, resolved)
		}
	}
}

func TestResolveDetectsEscapes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	for _, path := range []string{"../sibling.txt", "../../etc/passwd", "/etc/hostname", filepath.Join(root, "..", "outside.txt")} {
		_, outside, err := resolvePath(root, path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if !outside {
			t.Fatalf("%s was not detected as leaving the root", path)
		}
	}
}

func TestResolveFollowsSymlinksBeforeDeciding(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "project")
	secrets := filepath.Join(base, "secrets")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(secrets, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secrets, "key.txt"), []byte("k"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A link that lives inside the root but points out of it. Checking the
	// literal path would call this inside; it is a hole straight through.
	if err := os.Symlink(secrets, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Chdir(root)

	_, outside, err := resolvePath(root, "escape/key.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !outside {
		t.Fatal("a symlink out of the root was treated as inside it")
	}
}

func TestResolveHandlesAPathThatDoesNotExistYet(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	// write_file creates files, so the target usually does not exist. Its
	// nearest existing ancestor is what decides.
	resolved, outside, err := resolvePath(root, "new/dir/file.txt")
	if err != nil || outside {
		t.Fatalf("resolved=%s outside=%v err=%v", resolved, outside, err)
	}
	_, outside, err = resolvePath(root, "../new/dir/file.txt")
	if err != nil || !outside {
		t.Fatalf("an escaping new path was allowed: outside=%v err=%v", outside, err)
	}
}

func TestResolveWithNoRootConfinesNothing(t *testing.T) {
	// An empty root disables confinement, which is what tests and the
	// pre-existing behaviour rely on.
	_, outside, err := resolvePath("", "/etc/hostname")
	if err != nil || outside {
		t.Fatalf("outside=%v err=%v", outside, err)
	}
}

func TestRootItselfIsInside(t *testing.T) {
	root := t.TempDir()
	_, outside, err := resolvePath(root, root)
	if err != nil || outside {
		t.Fatalf("the root is not inside itself: outside=%v err=%v", outside, err)
	}
}

// The path a tool reports is the resolved one, not the one the model typed.
//
// This is what macOS was testing by accident: its temp directories live under
// /var, a symlink to /private/var, so every path a tool reported there differed
// from the raw t.TempDir() a test compared it against. Two tests passed on
// Linux and failed on macOS for a whole release because nothing on Linux
// exercised a symlink in the happy path. This does.
func TestTheReportedPathIsTheResolvedOne(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(root, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Chdir(root)

	var seen Request
	var hookPath string
	_, err = Execute(context.Background(), "write_file",
		`{"path":"link/a.txt","content":"x","purpose":"through a symlink"}`,
		Options{
			Root:  root,
			Guard: func(r Request) bool { seen = r; return true },
			PreWrite: func(_, path string) error {
				hookPath = path
				return nil
			},
		})
	if err != nil {
		t.Fatalf("write through a symlink: %v", err)
	}

	want := filepath.Join(real, "a.txt")
	// The confirmation is what a person approves, and the hook is what a
	// checkpoint backs up. Both have to name the file that is actually written.
	if seen.Path != want {
		t.Fatalf("the confirmation saw %q, want the resolved %q", seen.Path, want)
	}
	if hookPath != want {
		t.Fatalf("the pre-write hook saw %q, want the resolved %q", hookPath, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("nothing was written to the resolved path: %v", err)
	}
}
