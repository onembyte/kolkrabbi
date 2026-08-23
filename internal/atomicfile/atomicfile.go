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

// Write replaces path with data, atomically.
//
// A reader either sees the previous contents or the new ones, never a mixture
// and never an empty file. On success the data is on disk, not merely in the
// page cache.
func Write(path string, data []byte, perm os.FileMode) error {
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
	if err := tmp.Sync(); err != nil {
		return cleanup(fmt.Errorf("flushing %s to disk: %w", tmpName, err))
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
	if err := syncDir(dir); err != nil {
		return &DurabilityError{Path: path, Err: err}
	}
	return nil
}

// WriteJSON is the shape almost every caller wants: marshal, then replace.
// It exists so that a caller cannot accidentally truncate a good file and then
// discover the value would not marshal.
func WriteJSON(path string, v any, perm os.FileMode, marshal func(any) ([]byte, error)) error {
	b, err := marshal(v)
	if err != nil {
		return err
	}
	return Write(path, b, perm)
}
