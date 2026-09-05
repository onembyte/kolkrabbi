package term

import (
	"fmt"
	"os"
	"sync"

	xterm "golang.org/x/term"
)

const defaultHeight = 24

// Size reports the current output terminal dimensions. Width keeps honoring
// the existing COLUMNS fallback; height uses a conservative 24-row default.
func Size(output *os.File) (width, height int) {
	if output == nil {
		return Width(), defaultHeight
	}
	return sizeFor(int(output.Fd()), xterm.GetSize, Width())
}

func sizeFor(fd int, probe func(int) (int, int, error), fallbackWidth int) (width, height int) {
	width, height, err := probe(fd)
	if err != nil || width < 20 || width > 1000 || height < 5 || height > 1000 {
		return fallbackWidth, defaultHeight
	}
	return width, height
}

// EnterRaw disables the terminal driver's line buffering and echo until the
// returned cleanup is called. Cleanup is safe to call more than once and
// always returns the first restore result.
func EnterRaw(input *os.File) (func() error, error) {
	if input == nil {
		return nil, fmt.Errorf("entering raw terminal: nil input")
	}
	return enterRawWith(int(input.Fd()), func(fd int) (any, error) {
		return xterm.MakeRaw(fd)
	}, func(fd int, state any) error {
		rawState, ok := state.(*xterm.State)
		if !ok {
			return fmt.Errorf("restoring raw terminal: unexpected state %T", state)
		}
		return xterm.Restore(fd, rawState)
	})
}

func enterRawWith(
	fd int,
	makeRaw func(int) (any, error),
	restore func(int, any) error,
) (func() error, error) {
	state, err := makeRaw(fd)
	if err != nil {
		return nil, fmt.Errorf("entering raw terminal: %w", err)
	}
	var once sync.Once
	var restoreErr error
	return func() error {
		once.Do(func() {
			if err := restore(fd, state); err != nil {
				restoreErr = fmt.Errorf("restoring raw terminal: %w", err)
			}
		})
		return restoreErr
	}, nil
}

// ReadPassword reads one line from f with echo off, for a credential the user
// pastes. It is the only way kolk ever takes a key from a person at a
// terminal: a key typed after a command sits in the scrollback, in the shell's
// history when it came in on argv, and in `ps` while the process runs. The
// caller decides that f is a terminal; on a pipe it should read a line instead.
func ReadPassword(f *os.File) (string, error) {
	b, err := xterm.ReadPassword(int(f.Fd()))
	if err != nil {
		return "", err
	}
	return string(b), nil
}
