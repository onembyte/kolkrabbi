//go:build !darwin && !linux && !windows

package lock

import "os"

// Add an OS-backed implementation before claiming support on another target.
func tryLock(*os.File) (bool, error) { return false, ErrUnsupported }
func unlock(*os.File) error          { return ErrUnsupported }
