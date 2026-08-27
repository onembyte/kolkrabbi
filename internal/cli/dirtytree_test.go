package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUncommittedFilesNamesWhatIsChanged(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "--quiet", "--initial-branch=main"},
		{"config", "user.email", "t@example.invalid"},
		{"config", "user.name", "T"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "kept.txt"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "--quiet", "-m", "first"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}

	look := uncommittedFiles(dir)
	if files := look(context.Background()); len(files) != 0 {
		t.Fatalf("a clean tree reported %v", files)
	}

	if err := os.WriteFile(filepath.Join(dir, "kept.txt"), []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	files := look(context.Background())
	joined := strings.Join(files, " ")
	for _, want := range []string{"kept.txt", "new.txt"} {
		if !strings.Contains(joined, want) {
			t.Errorf("uncommitted files %v omit %s", files, want)
		}
	}
}

// A courtesy that can break a turn is a defect: everything about this fails
// quietly.
func TestUncommittedFilesSaysNothingOutsideARepository(t *testing.T) {
	if files := uncommittedFiles(t.TempDir())(context.Background()); files != nil {
		t.Errorf("a directory that is not a repository reported %v", files)
	}
}
