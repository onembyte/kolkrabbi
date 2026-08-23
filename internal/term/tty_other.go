//go:build !darwin && !linux && !windows

package term

// isTerminal is an honest stub for targets outside the supported CLI matrix.
// Add a native console probe before claiming terminal support on another OS.
func isTerminal(uintptr) bool { return false }
