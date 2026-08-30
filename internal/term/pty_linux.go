//go:build linux

package term

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// noCtty keeps opening the slave from making it this process's controlling
// terminal. The child claims it instead, via Setsid and Setctty.
const noCtty = unix.O_NOCTTY

// openPTY is the SysV sequence: open the multiplexer, clear the lock, then ask
// for the slave's number.
func openPTY() (*os.File, string, error) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, "", fmt.Errorf("open /dev/ptmx: %w", err)
	}
	fd := int(master.Fd())
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		_ = master.Close()
		return nil, "", fmt.Errorf("unlock pty: %w", err)
	}
	number, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		_ = master.Close()
		return nil, "", fmt.Errorf("name pty: %w", err)
	}
	return master, fmt.Sprintf("/dev/pts/%d", number), nil
}

// SetPTYSize tells the child how big its terminal is. The slave is used for the
// same reason as on darwin, so both platforms take one code path above.
func SetPTYSize(pty *PTY, width, height int) error {
	if pty == nil || pty.Slave == nil {
		return ErrNoPTY
	}
	return unix.IoctlSetWinsize(int(pty.Slave.Fd()), unix.TIOCSWINSZ, &unix.Winsize{
		Row: uint16(height), Col: uint16(width),
	})
}
