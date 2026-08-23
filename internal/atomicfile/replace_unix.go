//go:build !windows

package atomicfile

import "os"

// replace moves the finished temp file over the target. On POSIX, rename is
// atomic and silently replaces an existing file, which is the whole reason this
// package's approach works at all.
func replace(from, to string) error { return os.Rename(from, to) }

// syncDir makes the rename itself durable.
//
// Opening a directory read-only and calling Sync is the portable-across-Unix
// way to flush its entries. It fails on a few filesystems that do not support
// it; that is reported rather than hidden, but callers treat it as advisory
// because the data is already committed and visible by then.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	if closeErr := d.Close(); err == nil {
		err = closeErr
	}
	return err
}
