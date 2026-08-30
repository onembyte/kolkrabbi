package term

import (
	"errors"
	"os"
)

// A pty is how kolk runs a vendor's own CLI without leaving the session.
//
// The alternative it replaces was handing the whole terminal to the child, or
// opening a second window — and on a stock macOS there is no emulator binary on
// PATH at all, so the window path simply failed. A pty gives the child a
// terminal of its own that kolk owns both ends of: the child gets the raw-mode
// TTY its own UI needs, and kolk keeps the real terminal and stays running.
//
// The pty lives here because internal/arch permits golang.org/x/sys in exactly
// one package, and this is it. Everything above receives *os.File and never
// learns which ioctl opened it.

// ErrNoPTY reports a platform with no pty for kolk to open. Callers fall back
// to handing over the terminal rather than failing the login.
var ErrNoPTY = errors.New("this platform has no pty; the login needs its own terminal")

// PTY is a pseudo-terminal pair. Master is the end kolk reads and writes;
// Slave is the end the child process is given as its terminal.
type PTY struct {
	Master *os.File
	Slave  *os.File
}

// Close releases both ends. It is safe to call twice, and safe after the caller
// has already closed its own copy of the slave — which it should, once the
// child holds it, so that reading the master sees EOF when the child exits.
func (p *PTY) Close() error {
	var err error
	if p.Slave != nil {
		err = p.Slave.Close()
		p.Slave = nil
	}
	if p.Master != nil {
		if closeErr := p.Master.Close(); err == nil {
			err = closeErr
		}
		p.Master = nil
	}
	return err
}

// OpenPTY allocates a pseudo-terminal and sizes it.
//
// The size is applied after the slave is open, not before: on darwin a
// TIOCSWINSZ against the master alone returns ENOTTY, so sizing first looks
// like a working call and silently is not.
func OpenPTY(width, height int) (*PTY, error) {
	master, slaveName, err := openPTY()
	if err != nil {
		return nil, err
	}
	slave, err := os.OpenFile(slaveName, os.O_RDWR|noCtty, 0)
	if err != nil {
		_ = master.Close()
		return nil, err
	}
	pty := &PTY{Master: master, Slave: slave}
	if width > 0 && height > 0 {
		// A pty that reports 0x0 makes a full-screen child draw nothing, so a
		// failure here is worth reporting rather than continuing blind.
		if err := SetPTYSize(pty, width, height); err != nil {
			_ = pty.Close()
			return nil, err
		}
	}
	return pty, nil
}
