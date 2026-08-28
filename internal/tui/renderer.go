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
	eraseToLineEnd    = "\x1b[K"
)

// Renderer repaints one contiguous terminal region in the normal screen
// buffer. It neither clears scrollback nor enters the alternate screen.
type Renderer struct {
	out     io.Writer
	rows    int
	started bool
	closed  bool
	// lastView is the frame the rows count was computed for. A resize changes
	// how many physical rows that same frame occupies, and the clear sequence
	// has to erase the frame the terminal is actually showing.
	lastView string
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
//
// One write, and each row overwrites its predecessor in place. The earlier
// version erased the whole region and then painted it back, in two separate
// writes: the terminal was free to display the blank state between them, which
// is what made a resize flicker — a drag fires many repaints, and every one of
// them blanked the composer before redrawing it.
//
// So: no erase before drawing. Each line is written followed by erase-to-end-
// of-line, which clears whatever was longer than the new content without ever
// blanking the row. Only when the new frame is SHORTER than the last does
// anything get erased below, and by then the new frame is already on screen.
func (r *Renderer) Render(view string) error {
	var frame strings.Builder

	// Back to the top-left of the region this renderer owns.
	frame.WriteString("\r")
	if r.rows > 1 {
		fmt.Fprintf(&frame, "\x1b[%dA", r.rows-1)
	}

	rows := 0
	if view != "" {
		// Raw terminal mode disables the output post-processing that normally
		// turns LF into CRLF. Emit both explicitly so every repainted row starts
		// in column zero instead of staircasing across the screen.
		lines := strings.Split(view, "\n")
		rows = len(lines)
		for index, line := range lines {
			if index > 0 {
				frame.WriteString("\r\n")
			}
			frame.WriteString(line)
			frame.WriteString(eraseToLineEnd)
		}
	}
	// A shorter frame leaves rows of the previous one below the cursor.
	if rows < r.rows {
		frame.WriteString(eraseBelow)
	}

	if _, err := io.WriteString(r.out, frame.String()); err != nil {
		return err
	}
	r.rows = rows
	r.lastView = view
	return nil
}

// Resized recomputes how many physical rows the last frame occupies now that
// the terminal is width columns wide. Every frame is clipped to the width it
// was rendered at, so a row is one physical row until the terminal narrows and
// re-flows it onto several; clearing by the old count would then leave the
// top of the previous frame on screen above the new one.
func (r *Renderer) Resized(width int) {
	if r.lastView == "" || width <= 0 {
		return
	}
	rows := 0
	for _, line := range strings.Split(r.lastView, "\n") {
		cells := visibleWidth(line)
		if cells <= width {
			rows++
			continue
		}
		rows += (cells + width - 1) / width
	}
	r.rows = rows
}

// visibleWidth counts the cells a rendered row occupies: escape sequences take
// none, every other rune takes one. The frame is ASCII and box-drawing, all
// single-cell, so rune count is exact here; wide glyphs in user content would
// be undercounted by this and over-cleared by one row, which is harmless.
func visibleWidth(row string) int {
	cells := 0
	inEscape := false
	for _, r := range row {
		switch {
		case inEscape:
			if r >= 0x40 && r <= 0x7e {
				inEscape = false
			}
		case r == 0x1b:
			inEscape = true
		default:
			cells++
		}
	}
	return cells
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
