//go:build windows

package atomicfile

import (
	"errors"
	"os"
	"syscall"
	"time"
)

// Windows error numbers for "someone else has this file open". Compared
// numerically rather than by message text, which is localised.
const (
	errorAccessDenied     = syscall.Errno(5)
	errorSharingViolation = syscall.Errno(32)
	errorLockViolation    = syscall.Errno(33)
)

// replace moves the finished temp file over the target.
//
// Go's os.Rename on Windows uses MoveFileEx with MOVEFILE_REPLACE_EXISTING, so
// it does overwrite — but it fails outright if anything else has the target
// open without FILE_SHARE_DELETE. On Windows that "anything else" is routinely
// a virus scanner or a search indexer that opened the file microseconds ago and
// will be gone microseconds from now.
//
// So: retry briefly, with a backoff. This is the difference between kolk
// working on a corporate Windows laptop and kolk failing to save a session
// roughly once an hour for reasons nobody can reproduce.
func replace(from, to string) error {
	delay := time.Millisecond

	var err error
	for i := 0; i < 10; i++ {
		if err = os.Rename(from, to); err == nil {
			return nil
		}
		if !transient(err) {
			return err // a real error; retrying will not help
		}
		time.Sleep(delay)
		if delay < 100*time.Millisecond {
			delay *= 2
		}
	}
	return err
}

// transient reports whether the failure is another process holding the file
// open, rather than something retrying cannot fix.
func transient(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	return errno == errorSharingViolation || errno == errorAccessDenied || errno == errorLockViolation
}

// syncDir is a no-op on Windows: a directory cannot be opened as a file, so
// there is no handle to flush. The rename is durable by the time it returns.
func syncDir(string) error { return nil }
