//go:build !darwin && !linux

package term

import "os"

// noCtty has no meaning where there is no pty to open.
const noCtty = 0

// openPTY reports that this platform has none. Windows has ConPTY, which is a
// different enough interface that pretending otherwise here would be worse than
// saying so: the caller falls back to handing over the terminal.
func openPTY() (*os.File, string, error) { return nil, "", ErrNoPTY }

// SetPTYSize has nothing to size.
func SetPTYSize(*PTY, int, int) error { return ErrNoPTY }
