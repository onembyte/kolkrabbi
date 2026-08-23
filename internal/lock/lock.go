// Package lock serializes file-backed read-modify-write operations across
// independent kolk processes.
//
// A lock file is permanent metadata, not a temporary file. Close releases the
// OS lock but never removes its path: unlinking a locked file lets a second
// process create and lock a different inode while the first process still owns
// the old one.
package lock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	// ErrBusy means another process owns the requested lock.
	ErrBusy = errors.New("lock is busy")
	// ErrUnsupported means the current OS has no lock implementation yet.
	ErrUnsupported = errors.New("file locking is unsupported on this platform")
)

// BusyError describes a lock held by another process. PID is zero when the
// owner acquired the OS lock but its metadata could not be read yet.
type BusyError struct {
	Path string
	PID  int
}

func (e *BusyError) Error() string {
	if e.PID > 0 {
		return fmt.Sprintf("%s is locked by process %d", e.Path, e.PID)
	}
	return fmt.Sprintf("%s is locked by another process", e.Path)
}

func (e *BusyError) Unwrap() error { return ErrBusy }

// File is one held exclusive lock. Close releases it.
type File struct {
	mu   sync.Mutex
	file *os.File
}

// Try acquires path immediately or returns a BusyError.
func Try(path string) (*File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening lock %s: %w", path, err)
	}
	fail := func(cause error) (*File, error) {
		_ = f.Close()
		return nil, cause
	}

	// Repair a permissive file left by an older version or manual creation.
	if err := f.Chmod(0o600); err != nil {
		return fail(fmt.Errorf("setting lock permissions on %s: %w", path, err))
	}

	acquired, err := tryLock(f)
	if err != nil {
		return fail(fmt.Errorf("locking %s: %w", path, err))
	}
	if !acquired {
		return fail(&BusyError{Path: path, PID: readPID(path)})
	}

	if err := writePID(f); err != nil {
		_ = unlock(f)
		return fail(fmt.Errorf("recording lock owner in %s: %w", path, err))
	}
	return &File{file: f}, nil
}

// Acquire waits until path can be locked or ctx is done.
func Acquire(ctx context.Context, path string) (*File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	delay := 5 * time.Millisecond
	for {
		held, err := Try(path)
		if err == nil {
			return held, nil
		}
		if !errors.Is(err, ErrBusy) {
			return nil, err
		}

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, ctx.Err()
		case <-timer.C:
		}
		if delay < 50*time.Millisecond {
			delay *= 2
		}
	}
}

// Close releases the OS lock and descriptor. It is idempotent.
func (f *File) Close() error {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.file == nil {
		return nil
	}

	unlockErr := unlock(f.file)
	closeErr := f.file.Close()
	f.file = nil
	return errors.Join(unlockErr, closeErr)
}

func writePID(f *os.File) error {
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.Seek(0, 0); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(f, "%d\n", os.Getpid()); err != nil {
		return err
	}
	return f.Sync()
}

func readPID(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}
