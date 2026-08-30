package tui

import (
	"context"
	"errors"
	"io"
)

// ErrAlreadyAttached refuses a second child while one already owns the screen.
// Two children sharing one terminal is the problem this whole path exists to
// solve, so it must not be reachable from inside it.
var ErrAlreadyAttached = errors.New("something already owns this session's terminal")

// RunAttached hands the session's terminal to a child process and takes it back
// afterwards, without the session ending or a second window opening.
//
// The frame is parked first — erased, cursor shown — so the child draws onto a
// clean screen in the space the composer occupied, and repainted after, so the
// session comes back exactly as it was. In between, the read goroutine forwards
// raw bytes to the child instead of decoding them: there is still exactly one
// reader on the terminal, which is the whole trick. Two readers on one
// descriptor is what made the old design hand the session away instead.
//
// run is given the keyboard as a reader, the screen as a writer, and the
// terminal's current size. It is called on the caller's goroutine — a model
// worker, not the read loop — so the read loop keeps running and the session
// stays interruptible throughout.
func (r *Runtime) RunAttached(ctx context.Context, run func(in io.Reader, out io.Writer, width, height int) error) error {
	if run == nil {
		return nil
	}
	keys, sink := io.Pipe()

	r.mu.Lock()
	if r.attached != nil {
		r.mu.Unlock()
		_ = keys.Close()
		_ = sink.Close()
		return ErrAlreadyAttached
	}
	r.attached = sink
	width, height := r.sizeLocked()
	output := r.output
	// Park the frame while still holding the lock, so no repaint can land
	// between the decision to attach and the screen being cleared.
	r.renderer.Park()
	r.mu.Unlock()

	err := run(keys, output, width, height)

	r.mu.Lock()
	r.attached = nil
	// Close the write end first: the forwarding path has already dropped it, and
	// closing it unblocks any read the child left parked.
	_ = sink.Close()
	_ = keys.Close()
	// The child scrolled the screen by an unknown amount, so the renderer must
	// not try to repaint over rows it no longer owns.
	r.renderer.Resume()
	r.renderLocked()
	r.mu.Unlock()
	return err
}

// sizeLocked is the terminal's size, with the same defaults the renderer uses.
func (r *Runtime) sizeLocked() (int, int) {
	width, height := r.width(), r.height()
	if width <= 0 {
		width = defaultWidth
	}
	if height <= 0 {
		height = defaultHeight
	}
	return width, height
}
