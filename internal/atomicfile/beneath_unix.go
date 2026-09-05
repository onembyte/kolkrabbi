//go:build !windows

package atomicfile

import (
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// WriteBeneath replaces path with data, atomically, and only if every step
// from root down to it is a real directory right now. It is Write for a path
// an attacker -- or a careless background process -- may have swapped for a
// symbolic link between the moment a caller decided to write and the moment
// it writes.
//
// The walk never trusts a path string twice. The root is opened once, each
// component is opened relative to the previous directory descriptor with
// O_NOFOLLOW, and the final rename is relative to the parent descriptor. A
// component that has become a link fails with ELOOP (ENOTDIR on some kernels)
// and nothing is written; a link at the final component is replaced by the
// file, not followed, because rename(2) never follows its destination. A
// missing intermediate directory is created relative to its parent, so a
// restore can recreate a directory the caller removed.
func WriteBeneath(root, path string, data []byte, perm os.FileMode) error {
	components, err := beneathRel(root, path)
	if err != nil {
		return err
	}
	dirFd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("opening the root %s: %w", root, err)
	}
	defer func() { _ = unix.Close(dirFd) }()

	for _, component := range components[:len(components)-1] {
		next, err := openDirectoryAt(dirFd, component)
		if err != nil {
			return fmt.Errorf("%w: %s under %s: %w", errEscape, component, root, err)
		}
		_ = unix.Close(dirFd)
		dirFd = next
	}
	base := components[len(components)-1]

	// A unique temporary name in the same directory, created exclusively and
	// never through a link. EEXIST is retried with a new name; anything else is
	// the caller's problem to hear about.
	var fd int
	var tmp string
	for attempt := 0; ; attempt++ {
		tmp = fmt.Sprintf(".%s.%d.%d.tmp", base, os.Getpid(), time.Now().UnixNano()+int64(attempt))
		fd, err = unix.Openat(dirFd, tmp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, uint32(perm.Perm()))
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EEXIST) || attempt > 8 {
			return fmt.Errorf("creating a temporary file beside %s: %w", path, err)
		}
	}
	f := os.NewFile(uintptr(fd), tmp)
	cleanup := func(cause error) error {
		_ = f.Close()
		_ = unix.Unlinkat(dirFd, tmp, 0)
		return cause
	}
	// O_CREAT applies the umask; the exact mode is set before any content.
	if err := f.Chmod(perm.Perm()); err != nil {
		return cleanup(fmt.Errorf("setting permissions on %s: %w", path, err))
	}
	if _, err := f.Write(data); err != nil {
		return cleanup(fmt.Errorf("writing %s: %w", path, err))
	}
	if err := f.Sync(); err != nil {
		return cleanup(fmt.Errorf("flushing %s to disk: %w", path, err))
	}
	if err := f.Close(); err != nil {
		_ = unix.Unlinkat(dirFd, tmp, 0)
		return fmt.Errorf("closing %s: %w", path, err)
	}
	if err := unix.Renameat(dirFd, tmp, dirFd, base); err != nil {
		_ = unix.Unlinkat(dirFd, tmp, 0)
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	if err := unix.Fsync(dirFd); err != nil {
		return &DurabilityError{Path: path, Err: err}
	}
	return nil
}

// openDirectoryAt opens component as a directory relative to dirFd without
// following a link, creating it when it does not exist. A link where a
// directory should be is the escape this whole file exists to refuse.
func openDirectoryAt(dirFd int, component string) (int, error) {
	flags := unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC
	fd, err := unix.Openat(dirFd, component, flags, 0)
	if errors.Is(err, unix.ENOENT) {
		if mkErr := unix.Mkdirat(dirFd, component, 0o755); mkErr != nil && !errors.Is(mkErr, unix.EEXIST) {
			return -1, mkErr
		}
		fd, err = unix.Openat(dirFd, component, flags, 0)
	}
	if err != nil {
		if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
			return -1, fmt.Errorf("%s is a symbolic link", component)
		}
		return -1, err
	}
	return fd, nil
}
