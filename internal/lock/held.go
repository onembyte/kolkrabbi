package lock

import (
	"fmt"
	"os"
)

// Held reports whether somebody currently holds the lock at path, without
// taking it and without creating it.
//
// Try is the wrong tool for this question twice over. It creates the file, so
// asking about a thousand sessions would leave a thousand lock files behind —
// and it opens, chmods and writes a PID for each, which turns a listing into a
// few hundred milliseconds of syscalls.
//
// Existence is not the answer either: the file outlives the lock, so a session
// that ran and exited leaves one behind. The file is the cheap first filter and
// the lock itself is the answer.
func Held(path string) (bool, error) {
	file, err := os.OpenFile(path, os.O_RDONLY, 0)
	if os.IsNotExist(err) {
		// Nobody has ever held it, so nobody holds it now.
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("opening lock %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	acquired, err := tryLock(file)
	if err != nil {
		return false, fmt.Errorf("probing lock %s: %w", path, err)
	}
	if !acquired {
		return true, nil
	}
	// Taken only to find out it was free. Give it straight back rather than
	// relying on the close, so the release is where a reader looks for it.
	if err := unlock(file); err != nil {
		return false, fmt.Errorf("releasing probe of %s: %w", path, err)
	}
	return false, nil
}
