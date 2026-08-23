package paths

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// legacy names the prototype's layout: everything in the config directory,
// including the things that are state. Moving them is a one-time correction,
// not a feature, so it happens silently except for one line of notice.
var legacy = []struct {
	name string // what to report it as
	from func(Dirs) string
	to   func(Dirs) string
}{
	{"sessions", func(d Dirs) string { return filepath.Join(d.Config, "sessions") }, Dirs.Sessions},
	{"usage log", func(d Dirs) string { return filepath.Join(d.Config, "stats.jsonl") }, Dirs.StatsFile},
}

// Migrate moves prototype-era state out of the config directory, once.
//
// Three properties it is built around, because this is the one function in the
// tree that can destroy something irreplaceable:
//
//   - It never overwrites. If the destination already exists, the source is
//     left alone and reported, so two kolks racing or a half-finished earlier
//     move cannot merge two histories into one corrupted one.
//   - It never deletes on failure. A rename that fails across a filesystem
//     boundary falls back to copy-then-remove, and the remove only happens
//     after the copy has been verified.
//   - It is a no-op when config and data are the same directory, which is what
//     a KOLK_CONFIG_DIR=KOLK_DATA_DIR test setup produces.
//
// It returns the human-readable names of what moved, so the caller can print
// one line rather than a paragraph.
func (d Dirs) Migrate() ([]string, error) {
	if d.Config == d.Data {
		return nil, nil
	}

	var moved []string
	var problems []error

	for _, item := range legacy {
		from, to := item.from(d), item.to(d)

		if _, err := os.Lstat(from); err != nil {
			continue // nothing to move: the normal case, forever, after the first run
		}
		if _, err := os.Lstat(to); err == nil {
			// Both exist. Refuse rather than guess which history is real.
			problems = append(problems, fmt.Errorf(
				"%s exists in both the old and new location; kolk is using %s and left %s alone",
				item.name, to, from))
			continue
		}

		if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
			problems = append(problems, fmt.Errorf("preparing %s: %w", filepath.Dir(to), err))
			continue
		}
		if err := move(from, to); err != nil {
			problems = append(problems, fmt.Errorf("moving %s: %w", item.name, err))
			continue
		}
		moved = append(moved, item.name)
	}

	return moved, errors.Join(problems...)
}

// move renames, and falls back to copy-then-remove when rename cannot do it —
// which is not exotic: a home directory on one volume and an overridden
// KOLK_DATA_DIR on another is an ordinary setup, and rename cannot cross a
// filesystem boundary.
//
// It falls back on any rename failure rather than trying to recognise EXDEV,
// because the errno for "different filesystem" differs by platform and getting
// that detection subtly wrong would turn a recoverable move into a lost one.
// A rename that failed for a real reason — permissions, a read-only volume —
// fails again in the copy, and the copy's error is the one worth reading.
func move(from, to string) error {
	renameErr := os.Rename(from, to)
	if renameErr == nil {
		return nil
	}
	if err := copyTree(from, to); err != nil {
		// Leave the source untouched; a partial destination is removed so the
		// next run retries cleanly rather than tripping the "both exist" guard.
		_ = os.RemoveAll(to)
		// Both errors, both wrapped: the copy failure is what to read, but the
		// rename failure is what says whether this was a filesystem boundary
		// or something worse.
		return fmt.Errorf("%w (rename first failed with: %w)", err, renameErr)
	}
	return os.RemoveAll(from)
}

// copyTree copies a file or a directory tree, preserving the 0600/0700 modes
// that make credentials and sessions private.
func copyTree(from, to string) error {
	info, err := os.Lstat(from)
	if err != nil {
		return err
	}

	switch {
	case info.Mode()&os.ModeSymlink != 0:
		// A symlink in the state directory is not something kolk created, so
		// following it would move a file the user did not mean to move.
		return fmt.Errorf("%s is a symlink; move it yourself and re-run", from)

	case info.IsDir():
		if err := os.MkdirAll(to, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(from)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyTree(filepath.Join(from, e.Name()), filepath.Join(to, e.Name())); err != nil {
				return err
			}
		}
		return nil

	default:
		return copyFile(from, to, info.Mode().Perm())
	}
}

func copyFile(from, to string, perm os.FileMode) error {
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	dst, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		return err
	}
	// Sync before Close: the source is about to be deleted, and a copy that is
	// only in the page cache is not a copy.
	if err := dst.Sync(); err != nil {
		_ = dst.Close()
		return err
	}
	return dst.Close()
}
