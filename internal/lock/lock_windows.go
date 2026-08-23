//go:build windows

package lock

import "os"

// Windows becomes a required target at architecture migration step 13. Keep
// the boundary explicit until LockFileEx and its contention tests land there.
func tryLock(*os.File) (bool, error) { return false, ErrUnsupported }
func unlock(*os.File) error          { return ErrUnsupported }
