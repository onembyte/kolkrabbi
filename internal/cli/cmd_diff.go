package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/checkpoint"
	"github.com/onembyte/kolkrabbi/internal/diff"
	"github.com/onembyte/kolkrabbi/internal/redact"
)

// diffContext is how much unchanged code surrounds each hunk. More than a
// confirmation shows, because this is read to decide whether to keep a
// session's work rather than to approve one edit.
const diffContext = 3

// diffLinesPerFile bounds each file's diff. A session that rewrote twenty files
// is not something anyone reads in a terminal in full, and the truncation says
// what it hid.
const diffLinesPerFile = 120

// printSessionDiff shows what this session changed, as diffs.
//
// `/changes` lists paths and verbs, which answers "did it touch anything" and
// not "should I keep this". Deciding that means reading the actual change, and
// until now that meant leaving Kolkrabbi.
func (a *app) printSessionDiff(store *checkpoint.Store, only string) {
	paths := store.ChangedPaths()
	if len(paths) == 0 {
		fmt.Fprintln(a.stdout, "no file changes recorded this session.")
		return
	}
	if only != "" {
		paths = matchingPaths(paths, only)
		if len(paths) == 0 {
			fmt.Fprintf(a.stdout, "%s was not changed by this session.\n", only)
			return
		}
	}

	for _, path := range paths {
		a.printOneDiff(store, path)
	}
}

func (a *app) printOneDiff(store *checkpoint.Store, path string) {
	label := displayPath(path)
	original, existed, err := store.Original(path)
	if err != nil {
		fmt.Fprintf(a.stderr, "%s: %v\n", label, err)
		return
	}
	current, err := os.ReadFile(path)
	if err != nil {
		// Deleted since, or never written. Saying so beats an empty diff.
		fmt.Fprintf(a.stdout, "\n%s — gone: %v\n", label, err)
		return
	}

	if !existed {
		fmt.Fprintf(a.stdout, "\n%s — new file\n", label)
		fmt.Fprint(a.stdout, redact.Scrub(diff.Truncate(addedLines(string(current)), diffLinesPerFile)))
		return
	}
	body := diff.Unified(string(original), string(current), diffContext)
	if body == "" {
		// Touched, then put back. An empty diff under a heading reads as a bug.
		fmt.Fprintf(a.stdout, "\n%s — unchanged: the session edited it and it is back to where it started\n", label)
		return
	}
	fmt.Fprintf(a.stdout, "\n%s\n", label)
	// The backup exists so /undo can put the bytes back, not so /diff can print
	// them: the file may be the .env, and its diff is two secrets. Every rendered
	// line goes through the scrubber the transcript goes through (plan 32 §4).
	fmt.Fprint(a.stdout, redact.Scrub(diff.Truncate(body, diffLinesPerFile)))
}

// matchingPaths narrows to what the user named, matched on the tail of the
// path so `/diff agent.go` works without typing the directory.
func matchingPaths(paths []string, only string) []string {
	wanted := filepath.ToSlash(strings.TrimSpace(only))
	matched := make([]string, 0, 1)
	for _, path := range paths {
		slashed := filepath.ToSlash(path)
		if slashed == wanted || strings.HasSuffix(slashed, "/"+wanted) {
			matched = append(matched, path)
		}
	}
	return matched
}

// displayPath shows a path the way the person typing it would.
func displayPath(path string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return path
	}
	rel, err := filepath.Rel(cwd, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}
	return filepath.ToSlash(rel)
}

// addedLines renders a new file as the addition it is.
func addedLines(content string) string {
	if content == "" {
		return "+\n"
	}
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimSuffix(content, "\n"), "\n") {
		b.WriteString("+" + line + "\n")
	}
	return b.String()
}
