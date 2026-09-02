package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A shared scratch directory is not a project root. Without this boundary a
// stale /tmp/SAGA.md hijacks every unrelated directory below it, including
// Go's test directories and a person's temporary checkout.
func TestSagaArtifactDoesNotInheritFromWorldWritableAncestor(t *testing.T) {
	shared := t.TempDir()
	if err := os.Chmod(shared, 0o1777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shared, "SAGA.md"), []byte("# SAGA: other project\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(shared, "unrelated", "nested")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)

	path, err := sagaArtifactPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(nested, "SAGA.md"); path != want {
		t.Fatalf("saga artifact = %q, want isolated child path %q", path, want)
	}
}

func TestSagaArtifactStillInheritsFromNormalNonGitAncestor(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "SAGA.md"), []byte("# SAGA: local project\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "src", "nested")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)

	path, err := sagaArtifactPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "SAGA.md"); path != want {
		t.Fatalf("saga artifact = %q, want normal ancestor path %q", path, want)
	}
}

// projectTree builds a repository with a nested working directory and chdirs
// into the nested one, which is where an inline SAGA prompt is most likely to
// be entered by accident.
func projectTree(t *testing.T) (root, nested string) {
	t.Helper()
	root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	nested = filepath.Join(root, "internal", "cli")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)
	return root, nested
}

func TestSagaGoalWritesTheArtifactAtTheProjectRoot(t *testing.T) {
	root, nested := projectTree(t)
	a := &app{stdout: &strings.Builder{}, stderr: &strings.Builder{}}

	if err := a.saveSagaGoal("fix all tests"); err != nil {
		t.Fatalf("saveSagaGoal: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(root, "SAGA.md"))
	if err != nil {
		t.Fatalf("the saga artifact is not at the project root: %v", err)
	}
	if !strings.Contains(string(body), "- **Goal**: fix all tests") {
		t.Fatalf("artifact = %q", body)
	}
	if _, err := os.Stat(filepath.Join(nested, "SAGA.md")); err == nil {
		t.Fatal("saving a saga from a subdirectory littered that subdirectory with SAGA.md")
	}
}
