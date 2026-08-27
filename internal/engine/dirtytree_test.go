package engine

import (
	"context"
	"strings"
	"testing"
)

// A session that cannot see uncommitted changes gives advice about a tree that
// no longer exists (item 28). The names are enough — a diff is expensive in
// context and the model can read one when it needs to.
func TestDirtyTreeNoteNamesTheFiles(t *testing.T) {
	note := dirtyTreeNote([]string{"internal/engine/agent.go", "README.md"})
	for _, want := range []string{"internal/engine/agent.go", "README.md", "uncommitted"} {
		if !strings.Contains(note, want) {
			t.Errorf("the note omits %q: %q", want, note)
		}
	}
	if strings.Contains(note, "diff") {
		t.Errorf("the note promises a diff it does not carry: %q", note)
	}
}

func TestDirtyTreeNoteIsEmptyForACleanTree(t *testing.T) {
	if note := dirtyTreeNote(nil); note != "" {
		t.Errorf("a clean tree produced a note: %q", note)
	}
}

// A tree with three hundred changed files must not put three hundred paths in
// front of every turn: the useful fact is that the tree is dirty and roughly
// where, not an inventory.
func TestDirtyTreeNoteIsCapped(t *testing.T) {
	var many []string
	for i := 0; i < 300; i++ {
		many = append(many, "some/deep/path/file.go")
	}
	note := dirtyTreeNote(many)
	if strings.Count(note, "some/deep/path/file.go") > maxDirtyFilesNamed {
		t.Errorf("more than %d files were named:\n%s", maxDirtyFilesNamed, note)
	}
	if !strings.Contains(note, "more") {
		t.Errorf("the note does not say that it truncated: %q", note)
	}
}

// The port is optional: a project that is not a repository, or a machine with
// no git, runs turns exactly as before.
func TestATurnWithoutTheDirtyTreePortIsUnchanged(t *testing.T) {
	a := &Agent{}
	if note := a.dirtyTreePreamble(context.Background()); note != "" {
		t.Errorf("an agent with no DirtyFiles produced a note: %q", note)
	}
}

func TestTheDirtyTreePortIsAskedOncePerTurn(t *testing.T) {
	calls := 0
	a := &Agent{Options: Options{DirtyFiles: func(context.Context) []string {
		calls++
		return []string{"a.go"}
	}}}
	if note := a.dirtyTreePreamble(context.Background()); !strings.Contains(note, "a.go") {
		t.Errorf("the note does not name the changed file: %q", note)
	}
	if calls != 1 {
		t.Errorf("the port was called %d times for one turn", calls)
	}
}
