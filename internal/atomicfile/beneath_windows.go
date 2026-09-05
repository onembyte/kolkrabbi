//go:build windows

package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteBeneath on Windows is a resolve-then-write: the parent directory is
// resolved through its links and must land inside the root, a link at the path
// itself is removed rather than followed, and then Write replaces the file.
// There is no openat family to anchor the walk to a descriptor, so a swap
// between the check and the write is not excluded here the way it is on unix.
// Windows is cross-built and not a supported runtime; this is recorded rather
// than hidden.
func WriteBeneath(root, path string, data []byte, perm os.FileMode) error {
	if _, err := beneathRel(root, path); err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("resolving the directory of %s: %w", path, err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		parent = filepath.Dir(path)
	}
	if _, err := beneathRel(root, filepath.Join(parent, filepath.Base(path))); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("removing the link at %s: %w", path, err)
		}
	}
	return Write(path, data, perm)
}
