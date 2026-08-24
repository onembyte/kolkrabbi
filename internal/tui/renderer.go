package tui

import (
	"fmt"
	"io"
	"strings"
)

const (
	bracketedPasteOn  = "\x1b[?2004h"
	bracketedPasteOff = "\x1b[?2004l"
	hideCursor        = "\x1b[?25l"
	showCursor        = "\x1b[?25h"
	eraseBelow        = "\x1b[J"
)

// Renderer repaints one contiguous terminal region in the normal screen
// buffer. It neither clears scrollback nor enters the alternate screen.
type Renderer struct {
	out     io.Writer
	rows    int
	started bool
	closed  bool
}

// NewRenderer binds a renderer to one terminal writer.
func NewRenderer(out io.Writer) *Renderer { return &Renderer{out: out} }

// Start enables paste framing and uses a virtual cursor rendered by the model.
func (r *Renderer) Start() error {
	if r.started {
		return nil
	}
	if _, err := io.WriteString(r.out, bracketedPasteOn+hideCursor); err != nil {
		return err
	}
	r.started = true
	return nil
}

// Render replaces the exact rows owned by the previous frame.
func (r *Renderer) Render(view string) error {
	if r.rows > 0 {
		if _, err := io.WriteString(r.out, r.clearSequence()); err != nil {
			return err
		}
	}
	if view != "" {
		if _, err := io.WriteString(r.out, view); err != nil {
			return err
		}
		r.rows = strings.Count(view, "\n") + 1
	} else {
		r.rows = 0
	}
	return nil
}

// Close erases the owned region and restores terminal modes exactly once.
func (r *Renderer) Close() error {
	if r.closed {
		return nil
	}
	sequence := ""
	if r.rows > 0 {
		sequence = r.clearSequence()
	}
	sequence += showCursor + bracketedPasteOff
	if _, err := io.WriteString(r.out, sequence); err != nil {
		return err
	}
	r.rows = 0
	r.closed = true
	return nil
}

func (r *Renderer) clearSequence() string {
	sequence := "\r"
	if r.rows > 1 {
		sequence += fmt.Sprintf("\x1b[%dA", r.rows-1)
	}
	return sequence + eraseBelow
}
