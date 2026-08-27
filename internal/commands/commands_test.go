package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAFileIsACommand(t *testing.T) {
	project := t.TempDir()
	write(t, filepath.Join(project, ".kolk", "commands"), "review.md",
		"---\ndescription: review a diff\n---\nRead the staged diff and comment on naming.\n")

	found := Load(project, t.TempDir())
	if len(found) != 1 {
		t.Fatalf("loaded %d commands, want 1: %v", len(found), found)
	}
	if found[0].Name != "review" {
		t.Errorf("name = %q, want review (the filename is the name)", found[0].Name)
	}
	if found[0].Description != "review a diff" {
		t.Errorf("description = %q, want the front matter's", found[0].Description)
	}
	if strings.Contains(found[0].Body, "---") {
		t.Errorf("the front matter was left in the prompt:\n%s", found[0].Body)
	}
	if !strings.Contains(found[0].Body, "staged diff") {
		t.Errorf("the body is missing:\n%s", found[0].Body)
	}
}

// $ARGUMENTS is replaced by whatever followed the command; absent, the
// arguments are appended.
func TestArgumentsArePlacedOrAppended(t *testing.T) {
	placed := Command{Body: "Review $ARGUMENTS carefully."}
	if got := placed.Prompt("the auth module"); got != "Review the auth module carefully." {
		t.Errorf("prompt = %q", got)
	}
	appended := Command{Body: "Review the staged diff."}
	if got := appended.Prompt("quickly"); got != "Review the staged diff.\n\nquickly" {
		t.Errorf("prompt = %q, want the arguments appended", got)
	}
	if got := appended.Prompt(""); got != "Review the staged diff." {
		t.Errorf("no arguments should leave the body alone, got %q", got)
	}
	// Every occurrence, so a command can name its argument twice.
	twice := Command{Body: "$ARGUMENTS then $ARGUMENTS"}
	if got := twice.Prompt("x"); got != "x then x" {
		t.Errorf("prompt = %q, want both placeholders filled", got)
	}
}

// Project over user, "because it is nearer the work".
func TestTheProjectWinsANameClash(t *testing.T) {
	project, user := t.TempDir(), t.TempDir()
	write(t, filepath.Join(project, ".kolk", "commands"), "review.md", "project version")
	write(t, filepath.Join(user, "commands"), "review.md", "user version")

	found := Load(project, user)
	if len(found) != 1 {
		t.Fatalf("loaded %d, want the project's alone: %v", len(found), found)
	}
	if !strings.Contains(found[0].Body, "project") {
		t.Errorf("the user's file won: %q", found[0].Body)
	}
}

// Claude Code's directory is read, not converted: someone who already wrote
// them should not have to move them to try kolk.
func TestClaudeCommandsAreReadAsAFallback(t *testing.T) {
	project := t.TempDir()
	write(t, filepath.Join(project, ".claude", "commands"), "review.md", "claude version")
	found := Load(project, t.TempDir())
	if len(found) != 1 || !strings.Contains(found[0].Body, "claude") {
		t.Fatalf("the .claude fallback was not read: %v", found)
	}

	// ...but only when .kolk has no file of that name.
	write(t, filepath.Join(project, ".kolk", "commands"), "review.md", "kolk version")
	found = Load(project, t.TempDir())
	if len(found) != 1 || !strings.Contains(found[0].Body, "kolk") {
		t.Fatalf(".kolk did not win over .claude: %v", found)
	}
}

// A command file is a prompt, and a prompt that does not fit is worse than one
// that is cut: it costs the window before the work starts.
func TestAnEnormousCommandIsCappedAtALineBoundary(t *testing.T) {
	project := t.TempDir()
	body := strings.Repeat("a line of prompt\n", 8000)
	write(t, filepath.Join(project, ".kolk", "commands"), "big.md", body)

	found := Load(project, t.TempDir())
	if len(found) != 1 {
		t.Fatalf("loaded %d, want 1", len(found))
	}
	if len(found[0].Body) > maxCommandBytes {
		t.Errorf("body is %d bytes, over the %d cap", len(found[0].Body), maxCommandBytes)
	}
	if strings.HasSuffix(found[0].Body, "a line of pro") {
		t.Error("the body was cut mid-line rather than at a boundary")
	}
}

// A file cannot shadow a built-in: /undo must always be /undo.
func TestABuiltInNameIsRefused(t *testing.T) {
	project := t.TempDir()
	write(t, filepath.Join(project, ".kolk", "commands"), "undo.md", "not the real undo")
	if found := Load(project, t.TempDir()); len(found) != 0 {
		t.Errorf("a file was allowed to shadow a built-in slash command: %v", found)
	}
}

func TestNoDirectoriesMeansNoCommands(t *testing.T) {
	if found := Load(t.TempDir(), t.TempDir()); len(found) != 0 {
		t.Errorf("loaded %v from empty directories", found)
	}
}
