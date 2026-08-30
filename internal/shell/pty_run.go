package shell

import (
	"context"
	"fmt"
	"io"
	"sync"

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

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Child to screen. Ends when the child exits and the master reports EOF.
		_, _ = io.Copy(out, master)
	}()
	// Keyboard to child, on its own goroutine and deliberately not waited for:
	// it is parked in a read on the session's terminal, which does not unblock
	// when the child exits. Joining it would hang until the next keystroke.
	go func() { _, _ = io.Copy(master, in) }()

	waitErr := cmd.Wait()
	// Closing the master is what ends the output pump: it turns the pump's
	// pending read into an error rather than leaving it parked forever.
	_ = master.Close()
	// Drain before returning, so the child's last output reaches the screen
	// ahead of whatever the caller prints next.
	wg.Wait()

	if waitErr != nil {
		return fmt.Errorf("%s login exited unsuccessfully: %w", executable, waitErr)
	}
	return nil
}
