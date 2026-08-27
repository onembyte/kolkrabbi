package engine

import (
	"context"
	"fmt"
	"strings"
)

// maxDirtyFilesNamed bounds the list.
//
// A tree with three hundred changed files must not put three hundred paths in
// front of every turn. The useful fact is that the tree is dirty and roughly
// where; an inventory is what the model can ask for with a tool when it wants
// one.
const maxDirtyFilesNamed = 20

// dirtyTreePreamble is what a turn is told about uncommitted work before it
// starts, or "" when there is nothing to say.
//
// It is deliberately **not** part of the system prompt, and the reason is
// written a few files away: mutating the system prompt mid-session costs the
// provider's prompt cache, which is why loop wakeups are injected as user turns
// instead. Dirty state changes every turn — it is the worst possible thing to
// put somewhere that must stay stable — so it goes in beside the turn, the same
// way. That answers item 28's open question with a cost rather than a taste.
func (a *Agent) dirtyTreePreamble(ctx context.Context) string {
	if a.DirtyFiles == nil {
		return ""
	}
	return dirtyTreeNote(a.DirtyFiles(ctx))
}

// dirtyTreeNote renders the names, and only the names.
//
// Not the diff: a diff is expensive in context and the model can read one when
// it needs to. What a session needs to know before it advises is *that* these
// files differ from the last commit, because advice about a tree that no longer
// exists is worse than no advice.
func dirtyTreeNote(files []string) string {
	if len(files) == 0 {
		return ""
	}
	named := files
	extra := 0
	if len(named) > maxDirtyFilesNamed {
		extra = len(named) - maxDirtyFilesNamed
		named = named[:maxDirtyFilesNamed]
	}

	var note strings.Builder
	note.WriteString("[working tree] These files have uncommitted changes:\n")
	for _, file := range named {
		note.WriteString("  " + file + "\n")
	}
	if extra > 0 {
		fmt.Fprintf(&note, "  …and %d more\n", extra)
	}
	return strings.TrimRight(note.String(), "\n")
}
