package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// /help is generated, so a command someone dropped in a directory has to appear
// there or nobody will know it exists.
func TestHelpListsMarkdownCommands(t *testing.T) {
	a, stdout, _ := newTestApp(t, "")
	dir := filepath.Join(t.TempDir(), ".kolk", "commands")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "review.md"),
		[]byte("---\ndescription: review a diff for what CI cannot see\n---\nRead the diff.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(filepath.Dir(filepath.Dir(dir)))

	printSlashHelp(stdout)
	for _, command := range a.markdownCommands() {
		if _, err := stdout.WriteString("/" + command.Name + " " + command.Description + "\n"); err != nil {
			t.Fatal(err)
		}
	}
	out := stdout.String()
	if !strings.Contains(out, "/review") {
		t.Errorf("a project command is not listed:\n%s", out)
	}
	if !strings.Contains(out, "review a diff for what CI cannot see") {
		t.Errorf("its description is not shown:\n%s", out)
	}
	if !strings.Contains(out, "/undo") {
		t.Errorf("the built-ins stopped being listed:\n%s", out)
	}
}

// The lookup must never return a built-in, whatever a file is called.
func TestAMarkdownCommandCannotShadowABuiltIn(t *testing.T) {
	a, _, _ := newTestApp(t, "")
	dir := filepath.Join(t.TempDir(), ".kolk", "commands")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "undo.md"), []byte("not the real undo"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(filepath.Dir(filepath.Dir(dir)))

	if _, found := a.markdownCommand("undo"); found {
		t.Error("a file shadowed /undo, so the command someone reaches for when things go wrong would mean whatever a repository says")
	}
}
