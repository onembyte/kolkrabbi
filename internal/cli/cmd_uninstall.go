package cli

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/paths"
	"github.com/onembyte/kolkrabbi/internal/shell"
)

// Uninstalling has to be as easy as installing, and it has to be honest about
// what it takes with it. One command that lists every path it will remove, with
// what each one holds and how big it is, then asks once.
//
// Nothing is guessed: the directories come from the same resolver the rest of
// kolk uses, so an install steered by KOLK_DATA_DIR is removed from where it
// actually is rather than from where the default would have put it.

// removal is one path uninstall would delete.
type removal struct {
	path  string
	what  string
	bytes int64
	// keep marks state a person may want to survive a reinstall: the API key,
	// saved sessions, settings. --keep-data spares exactly these.
	keep bool
}

func (a *app) runUninstall(_ context.Context, args []string) error {
	var assumeYes, keepData bool
	for _, arg := range args {
		switch arg {
		case "--yes", "-y":
			assumeYes = true
		case "--keep-data":
			keepData = true
		default:
			return usagef("%s", usageLine("uninstall"))
		}
	}

	dirs, err := a.locate()
	if err != nil {
		return err
	}

	planned := uninstallPlan(dirs, keepData)
	binary, binaryErr := shell.SelfPath()
	if binaryErr == nil {
		planned = append(planned, removal{path: binary, what: "the kolk binary", bytes: pathSize(binary)})
	}

	if len(planned) == 0 {
		fmt.Fprintln(a.stdout, "Nothing to remove: kolk has no files on this machine.")
		return nil
	}

	fmt.Fprintln(a.stdout, "This will remove:")
	var total int64
	for _, item := range planned {
		total += item.bytes
		fmt.Fprintf(a.stdout, "  %-46s %8s  %s\n", item.path, humanBytes(item.bytes), item.what)
	}
	fmt.Fprintf(a.stdout, "  %-46s %8s\n", "total", humanBytes(total))
	if keepData {
		fmt.Fprintln(a.stdout, "\nKept: your API key, settings and saved sessions.")
	} else {
		// Said plainly, because this is the part that cannot be undone and the
		// part people are surprised by.
		fmt.Fprintln(a.stdout, "\nThis includes your API key, settings and saved sessions.")
		fmt.Fprintln(a.stdout, "Use --keep-data to leave them for a later reinstall.")
	}

	if !assumeYes && !a.confirmed("Remove them?") {
		fmt.Fprintln(a.stdout, "\nNothing was removed.")
		return nil
	}

	// The binary goes last. If a directory cannot be removed, the command that
	// reports it is still on disk to be run again.
	var failures []string
	for _, item := range planned {
		if err := removePath(item.path); err != nil {
			failures = append(failures, fmt.Sprintf("  %s: %v", item.path, err))
		}
	}

	fmt.Fprintln(a.stdout)
	if len(failures) > 0 {
		fmt.Fprintln(a.stdout, "Some paths could not be removed:")
		for _, failure := range failures {
			fmt.Fprintln(a.stdout, failure)
		}
		return fmt.Errorf("uninstall incomplete: %d path(s) remain", len(failures))
	}
	fmt.Fprintln(a.stdout, "kolk is uninstalled.")
	if binaryErr == nil {
		if dir := filepath.Dir(binary); dir != "" {
			fmt.Fprintf(a.stdout, "Nothing was changed outside kolk's own files; %s is untouched otherwise.\n", dir)
		}
	}
	return nil
}

// uninstallPlan lists kolk's directories that exist, newest concern first. A
// directory that is not there is not mentioned: a list of paths that do not
// exist reads as a threat to delete things that were never kolk's.
func uninstallPlan(dirs paths.Dirs, keepData bool) []removal {
	candidates := []removal{
		{path: dirs.Config, what: "settings and your notes", keep: true},
		{path: dirs.Data, what: "API key, sessions, usage log", keep: true},
		{path: dirs.Cache, what: "cached model catalogue", keep: false},
	}
	var planned []removal
	for _, item := range candidates {
		if item.path == "" || (keepData && item.keep) {
			continue
		}
		if _, err := os.Stat(item.path); err != nil {
			continue
		}
		item.bytes = pathSize(item.path)
		planned = append(planned, item)
	}
	return planned
}

// removePath deletes a file or a whole directory.
//
// The rename fallback is for Windows, which refuses to unlink a running
// executable but does allow renaming it. Leaving `kolk.exe.old` beside the
// original is worse than nothing only if nobody says so, so the caller reports
// what remains.
func removePath(path string) error {
	if err := os.RemoveAll(path); err == nil {
		return nil
	} else if !isBusy(err) {
		return err
	}
	stale := path + ".old"
	_ = os.Remove(stale)
	if err := os.Rename(path, stale); err != nil {
		return err
	}
	return fmt.Errorf("in use, renamed to %s — delete it after this process exits", filepath.Base(stale))
}

func isBusy(err error) bool {
	// Matched on text rather than errno: the busy-file error differs by
	// platform and none of them is portable enough to name here.
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "text file busy") ||
		strings.Contains(message, "being used by another process") ||
		strings.Contains(message, "access is denied")
}

// pathSize is the total bytes at path, following nothing. An unreadable entry
// counts as zero rather than failing the listing: a size is a courtesy, and a
// number nobody can produce should not stop an uninstall.
func pathSize(path string) int64 {
	info, err := os.Lstat(path)
	if err != nil {
		return 0
	}
	if !info.IsDir() {
		return info.Size()
	}
	var total int64
	_ = filepath.WalkDir(path, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil //nolint:nilerr // an unreadable entry is skipped, not fatal
		}
		if info, err := entry.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}
