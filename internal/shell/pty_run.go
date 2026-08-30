package shell

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/onembyte/kolkrabbi/internal/term"
)

// RunInSession runs a vendor CLI on a pty that kolk owns, so a provider login
// happens inside the running session instead of somewhere else.
//
// What this replaces: handing the whole terminal to the child, or opening a
// second window. The window path cannot work on a stock macOS at all — there is
// no emulator binary on PATH — and the handover path ends the session. Neither
// is what someone means by "sign me in".
//
// The child gets a real terminal, which it needs: `claude auth login` and
// `ollama signin` are full-screen UIs that refuse to run on a pipe. Kolk keeps
// the real terminal and copies bytes between the two.
//
// in and out are the session's own terminal, already in raw mode, with the
// frame parked by the caller. Nothing is interpreted on the way through: kolk
// is a wire here, not a reader, which is what keeps the promise that it never
// sees the credential.

// ptyDrainGrace gives a descendant that still holds the slave a short chance
// to release it after the direct child exits. The normal path reaches EOF on
// its own; this bound keeps a detached child from freezing the login forever.
const ptyDrainGrace = time.Second

func RunInSession(ctx context.Context, executable string, args []string, in io.Reader, out io.Writer, width, height int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := LookPath(executable)
	if err != nil {
		return err
	}
	pty, err := term.OpenPTY(width, height)
	if err != nil {
		return err
	}
	defer func() { _ = pty.Close() }()

	cmd := commandOnPTY(ctx, path, args, pty.Slave)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s login did not start: %w", executable, err)
	}
	// The parent's copy of the slave goes now. Without this, reading the master
	// never sees EOF, because this process still holds the child's terminal
	// open and the copy below would hang after the child exits.
	_ = pty.Slave.Close()
	pty.Slave = nil

	// The pumps hold the master through a local, not through the struct. Closing
	// it below by clearing pty.Master would be a write to a field two goroutines
	// are reading, which is a data race the detector catches — and in a release
	// build, a nil dereference in whichever pump lost.
	master := pty.Master

	outputDone := make(chan struct{})
	go func() {
		defer close(outputDone)
		// Child to screen. Ends when the child exits and the master reports EOF.
		_, _ = io.Copy(out, master)
	}()
	// Keyboard to child, on its own goroutine and deliberately not waited for:
	// it is parked in a read on the session's terminal, which does not unblock
	// when the child exits. Joining it would hang until the next keystroke.
	go func() { _, _ = io.Copy(master, in) }()

	waitErr := cmd.Wait()
	// The child may have exited before the output pump was scheduled. Let the
	// pump observe the slave's EOF and drain bytes already written to the pty
	// before closing the master. If a descendant keeps the slave open, the
	// bounded fallback still guarantees that login shutdown cannot hang.
	timer := time.NewTimer(ptyDrainGrace)
	select {
	case <-outputDone:
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	case <-timer.C:
		_ = master.Close()
		<-outputDone
	}
	_ = master.Close()

	if waitErr != nil {
		return fmt.Errorf("%s login exited unsuccessfully: %w", executable, waitErr)
	}
	return nil
}
