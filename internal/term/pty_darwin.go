//go:build darwin

package term

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// noCtty keeps opening the slave from making it this process's controlling
// terminal. The child claims it instead, via Setsid and Setctty.
const noCtty = unix.O_NOCTTY

// openPTY is the BSD sequence: open the multiplexer, grant and unlock the
// slave, then name it.
//
// The name is derived from the device number rather than from TIOCPTYGNAME,
// because the ioctl that returns a name wants a 128-byte buffer and reading it
// back would need unsafe. Minor(rdev) gives the same answer with neither cgo
// nor unsafe, which is what keeps this package inside the repository's rules.
func openPTY() (*os.File, string, error) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, "", fmt.Errorf("open /dev/ptmx: %w", err)
	}
	fd := int(master.Fd())
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYGRANT, 0); err != nil {
		_ = master.Close()
		return nil, "", fmt.Errorf("grant pty: %w", err)
	}
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYUNLK, 0); err != nil {
		_ = master.Close()
		return nil, "", fmt.Errorf("unlock pty: %w", err)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		_ = master.Close()
		return nil, "", fmt.Errorf("stat pty: %w", err)
	}
	return master, fmt.Sprintf("/dev/ttys%03d", unix.Minor(uint64(stat.Rdev))), nil
}

// SetPTYSize tells the child how big its terminal is. It is applied to the
// slave: on darwin the master alone answers TIOCSWINSZ with ENOTTY.
func SetPTYSize(pty *PTY, width, height int) error {
	if pty == nil || pty.Slave == nil {
		return ErrNoPTY
	}
	return unix.IoctlSetWinsize(int(pty.Slave.Fd()), unix.TIOCSWINSZ, &unix.Winsize{
		Row: uint16(height), Col: uint16(width),
	})
}
