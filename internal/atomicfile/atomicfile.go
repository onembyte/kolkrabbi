// Package atomicfile replaces a file's contents in one step, or not at all.
//
// The naive version — write to "x.tmp", rename over "x" — is what the
// prototype did, and it has three holes that only show up when it matters:
//
//   - No fsync. rename is atomic with respect to other processes, but not with
//     respect to power loss: on several filesystems the metadata operation can
//     land before the data, leaving a zero-length or torn file after a crash.
//     A session transcript that empties itself on a laptop losing power is a
//     bug someone reports once and never trusts the tool again after.
//   - A fixed temp name. Two kolk processes saving the same session write the
//     same "x.tmp" and shred each other's data. That is not exotic — a REPL in
//     one terminal and `kolk -p` in another is an ordinary Tuesday.
//   - No directory sync. The rename itself is only durable once the directory
//     entry is on disk.
//
// Every write in this tree that must not be observed half-finished goes
// through here.
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
)

// DurabilityError means the replacement is already committed and visible,
// but syncing its directory entry failed. Callers must not report this as an
// untouched write failure: rolling back would require a second mutation and
// could be less durable than the committed file.
type DurabilityError struct {
	Path string
	Err  error
}

func (e *DurabilityError) Error() string {
	return fmt.Sprintf("%s was replaced, but its directory could not be synced: %v", e.Path, e.Err)
}

func (e *DurabilityError) Unwrap() error { return e.Err }

// WriteOptions tunes one replacement. Its zero value is what every caller had
// before options existed, so a caller that does not care keeps the strongest
// guarantee by default.
type WriteOptions struct {
	// SkipDirSync leaves the directory entry unsynced after the rename.
	//
	// The file's own bytes are still fsynced and the rename is still atomic,
	// so no reader ever sees a torn or empty file and no crashed process loses
	// anything: what is given up is survival of a power cut in the window
	// before the OS flushes the directory, where the *previous* contents come
	// back instead of the new ones. Only a write that will be repeated shortly
	// may ask for this — the engine's interval save between two turn
	// boundaries (OPTIMIZATION_PLAN.md O3), never a boundary itself.
	SkipDirSync bool
	// SkipFileSync leaves the new contents in the page cache rather than
	// fsyncing them before the rename.
	//
	// The replacement is still atomic — a reader sees the old file or the new
	// one, never a mixture — and a crashed process still loses nothing,
	// because the page cache outlives it. What is given up is survival of a
	// power cut, which is only acceptable for a file that can be rebuilt from
	// one that was written durably: the session header beside a transcript
	// (OPTIMIZATION_PLAN.md O6). Never for anything a person could not
	// reconstruct.
	SkipFileSync bool
}

// Write replaces path with data, atomically.
//
// A reader either sees the previous contents or the new ones, never a mixture
// and never an empty file. On success the data is on disk, not merely in the
// page cache.
//
// The signature stays three arguments rather than growing a variadic option:
// internal/selfupdate and the saga executor hold Write as a
// func(string, []byte, os.FileMode) error, and a seam that a fake can stand in
// for is worth more than one spelling for two callers.
func Write(path string, data []byte, perm os.FileMode) error {
	return WriteWith(path, data, perm, WriteOptions{})
}

// WriteWith is Write with the durability of the rename made explicit.
func WriteWith(path string, data []byte, perm os.FileMode, opts WriteOptions) error {
	dir := filepath.Dir(path)

	// The temp file must be in the same directory as the target: rename cannot
	// cross a filesystem boundary, and /tmp very often is one.
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("creating a temporary file next to %s: %w", path, err)
	}
	tmpName := tmp.Name()

	// From here, every failure removes the temp file. Leaving debris beside a
	// session directory is how a "sessions" listing fills with junk.
	cleanup := func(cause error) error {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return cause
	}

	// CreateTemp always makes the file 0600. Widen or narrow it deliberately,
	// before it has any content, so it is never briefly readable at the wrong
	// mode with the real data in it.
	if err := tmp.Chmod(perm); err != nil {
		return cleanup(fmt.Errorf("setting permissions on %s: %w", tmpName, err))
	}
	if _, err := tmp.Write(data); err != nil {
		return cleanup(fmt.Errorf("writing %s: %w", tmpName, err))
	}
	if !opts.SkipFileSync {
		if err := tmp.Sync(); err != nil {
			return cleanup(fmt.Errorf("flushing %s to disk: %w", tmpName, err))
		}
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("closing %s: %w", tmpName, err)
	}

	if err := replace(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("replacing %s: %w", path, err)
	}

	// The rename is only durable once the directory entry is. A failure here
	// is not worth failing the write over — the data is committed and visible;
	// only its survival of an immediate power loss is in question — so it is
	// returned and callers may choose to ignore it.
	if opts.SkipDirSync {
		return nil
	}
	if err := syncDir(dir); err != nil {
		return &DurabilityError{Path: path, Err: err}
	}
	return nil
}

// WriteJSON is the shape almost every caller wants: marshal, then replace.
// It exists so that a caller cannot accidentally truncate a good file and then
