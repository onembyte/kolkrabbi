package cli

import (
	"context"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/shell"
)

// uncommittedFiles builds the look the engine asks for at the start of a turn.
//
// It reuses what the saga already does — `git status --porcelain` — rather than
// growing a second way to ask the same question. It reads the **user's own**
// repository, which is a different thing from the shadow store's `GIT_DIR`:
// that one records snapshots, this one reports what the person has not
// committed.
//
// Everything about it fails quietly. A directory that is not a repository, a
// machine without git, a command that errors: all mean "nothing to say", never
// a failed turn. Dirty-tree awareness is a courtesy, and a courtesy that can
// break a turn is a defect.
func uncommittedFiles(root string) func(context.Context) []string {
	sh := shell.New()
	return func(ctx context.Context) []string {
		result, err := sh.Run(ctx, shell.Cmd{Command: "git status --porcelain", Dir: root})
		if err != nil || !result.OK() || result.ExitCode != 0 {
			return nil
		}
		var files []string
		for _, line := range strings.Split(result.Output, "\n") {
			if len(line) < 4 {
				continue
			}
			// "XY path", and for a rename "XY old -> new": the new name is the
			// one that exists on disk now.
			path := strings.TrimSpace(line[3:])
			if arrow := strings.Index(path, " -> "); arrow >= 0 {
				path = path[arrow+4:]
			}
			if path != "" {
				files = append(files, path)
			}
		}
		return files
	}
}
