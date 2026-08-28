package tui

import (
	"bytes"
	"unicode/utf8"
)

// KeyKind is one decoded editor action. Terminal byte parsing is deliberately
// separate from draft mutation so both halves can be fuzzed deterministically.
type KeyKind uint8

const (
	KeyText KeyKind = iota + 1
	KeyPaste
	KeyEnter
	KeyNewline
	KeyBackspace
	KeyDelete
	KeyLeft
	KeyRight
	KeyHome
	KeyEnd
	KeyUp
	KeyDown
	KeyInterrupt
	KeyEOF
	KeyTab
	KeyShiftTab
	KeyPageUp
	KeyPageDown
)

// Key carries text only for KeyText and KeyPaste.
type Key struct {
	Kind KeyKind
	Text string
}

// EditResult describes an effect the outer terminal loop must handle.
type EditResult struct {
	Changed   bool
	Submit    bool
	Submitted string
	Interrupt bool
	Exit      bool
}

// Editor owns the exact draft, logical cursor, and bounded process history.
// It contains no terminal I/O and never trims submitted input.
type Editor struct {
	draft        []rune
	cursor       int
	maxRunes     int
	history      []string
	historyIndex int
	pending      string
}

// NewEditor returns an empty editor. maxRunes bounds one draft; non-positive
// values use the interactive 64 Ki-rune safety ceiling.
func NewEditor(maxRunes int) *Editor {
	if maxRunes <= 0 {
		maxRunes = 64 * 1024
	}
	return &Editor{maxRunes: maxRunes, historyIndex: -1}
}

// Draft returns the exact current input.
func (e *Editor) Draft() string { return string(e.draft) }

// Cursor returns the logical rune offset, including newline runes.
func (e *Editor) Cursor() int { return e.cursor }

// Update applies one decoded key.
func (e *Editor) Update(key Key) EditResult {
	switch key.Kind {
	case KeyText, KeyPaste:
		return EditResult{Changed: e.insert([]rune(key.Text))}
	case KeyNewline:
		return EditResult{Changed: e.insert([]rune{'\n'})}
	case KeyBackspace:
		if e.cursor == 0 {
			return EditResult{}
		}
		copy(e.draft[e.cursor-1:], e.draft[e.cursor:])
		e.draft = e.draft[:len(e.draft)-1]
		e.cursor--
		e.leaveHistory()
		return EditResult{Changed: true}
	case KeyDelete:
		if e.cursor >= len(e.draft) {
			return EditResult{}
		}
		copy(e.draft[e.cursor:], e.draft[e.cursor+1:])
		e.draft = e.draft[:len(e.draft)-1]
		e.leaveHistory()
		return EditResult{Changed: true}
	case KeyLeft:
		if e.cursor > 0 {
			e.cursor--
			return EditResult{Changed: true}
		}
	case KeyRight:
		if e.cursor < len(e.draft) {
			e.cursor++
			return EditResult{Changed: true}
		}
	case KeyHome:
		start, _ := lineBounds(e.draft, e.cursor)
		if e.cursor != start {
			e.cursor = start
			return EditResult{Changed: true}
		}
	case KeyEnd:
		_, end := lineBounds(e.draft, e.cursor)
		if e.cursor != end {
			e.cursor = end
			return EditResult{Changed: true}
		}
	case KeyUp:
		if len(e.draft) == 0 || e.historyIndex >= 0 {
			return EditResult{Changed: e.historyUp()}
		}
		return EditResult{Changed: e.moveVertical(-1)}
	case KeyDown:
		if e.historyIndex >= 0 {
			return EditResult{Changed: e.historyDown()}
		}
		return EditResult{Changed: e.moveVertical(1)}
	case KeyEnter:
		if len(e.draft) == 0 {
			return EditResult{}
		}
		submitted := string(e.draft)
		e.addHistory(submitted)
		e.draft = e.draft[:0]
		e.cursor = 0
		e.historyIndex = -1
		e.pending = ""
		return EditResult{Changed: true, Submit: true, Submitted: submitted}
	case KeyInterrupt:
		return EditResult{Interrupt: true}
	case KeyEOF:
		return EditResult{Exit: true}
	}
	return EditResult{}
}

func (e *Editor) insert(input []rune) bool {
	available := e.maxRunes - len(e.draft)
	if available <= 0 || len(input) == 0 {
		return false
	}
	if len(input) > available {
		input = input[:available]
	}
	e.draft = append(e.draft, make([]rune, len(input))...)
	copy(e.draft[e.cursor+len(input):], e.draft[e.cursor:len(e.draft)-len(input)])
	copy(e.draft[e.cursor:], input)
	e.cursor += len(input)
	e.leaveHistory()
	return true
}

func (e *Editor) leaveHistory() {
	if e.historyIndex >= 0 {
		e.historyIndex = -1
		e.pending = ""
	}
}

func (e *Editor) addHistory(value string) {
	if len(e.history) == 0 || e.history[len(e.history)-1] != value {
		e.history = append(e.history, value)
	}
	if len(e.history) > 100 {
		copy(e.history, e.history[len(e.history)-100:])
		e.history = e.history[:100]
	}
}

func (e *Editor) historyUp() bool {
	if len(e.history) == 0 {
		return false
	}
	if e.historyIndex < 0 {
		e.pending = string(e.draft)
		e.historyIndex = len(e.history) - 1
	} else if e.historyIndex > 0 {
		e.historyIndex--
	} else {
		return false
	}
	e.setDraft(e.history[e.historyIndex])
	return true
}

func (e *Editor) historyDown() bool {
	if e.historyIndex < 0 {
		return false
	}
	if e.historyIndex < len(e.history)-1 {
		e.historyIndex++
		e.setDraft(e.history[e.historyIndex])
		return true
	}
	e.historyIndex = -1
	e.setDraft(e.pending)
	e.pending = ""
	return true
}

func (e *Editor) setDraft(value string) {
	e.draft = append(e.draft[:0], []rune(value)...)
	e.cursor = len(e.draft)
}

func (e *Editor) clearDraft() bool {
	changed := len(e.draft) > 0 || e.cursor != 0
	e.draft = e.draft[:0]
	e.cursor = 0
	e.historyIndex = -1
	e.pending = ""
	return changed
}

func (e *Editor) moveVertical(direction int) bool {
	start, end := lineBounds(e.draft, e.cursor)
	column := e.cursor - start
	if direction < 0 {
		if start == 0 {
			return false
		}
		previousEnd := start - 1
		previousStart, _ := lineBounds(e.draft, previousEnd)
		e.cursor = min(previousStart+column, previousEnd)
		return true
	}
	if end == len(e.draft) {
		return false
	}
	nextStart := end + 1
	_, nextEnd := lineBounds(e.draft, nextStart)
	e.cursor = min(nextStart+column, nextEnd)
	return true
}

func lineBounds(text []rune, cursor int) (start, end int) {
	cursor = min(max(cursor, 0), len(text))
	start = cursor
	for start > 0 && text[start-1] != '\n' {
		start--
	}
	end = cursor
	for end < len(text) && text[end] != '\n' {
		end++
	}
	return start, end
}

var (
	pasteStart = []byte("\x1b[200~")
	pasteEnd   = []byte("\x1b[201~")
)

// Decoder incrementally translates terminal bytes into editor keys. Partial
// UTF-8 and escape sequences remain buffered between Feed calls.
type Decoder struct {
	pending  []byte
	pasting  bool
	pasteBuf []byte
}

// NewDecoder returns an empty incremental decoder.
func NewDecoder() *Decoder { return &Decoder{} }

// Feed accepts one arbitrary read chunk.
func (d *Decoder) Feed(chunk []byte) []Key {
	d.pending = append(d.pending, chunk...)
	var keys []Key
	var text bytes.Buffer
	flushText := func() {
		if text.Len() > 0 {
			keys = append(keys, Key{Kind: KeyText, Text: text.String()})
			text.Reset()
		}
	}

	for len(d.pending) > 0 {
		if d.pasting {
			if end := bytes.Index(d.pending, pasteEnd); end >= 0 {
				d.pasteBuf = append(d.pasteBuf, d.pending[:end]...)
				d.pending = d.pending[end+len(pasteEnd):]
				flushText()
				keys = append(keys, Key{Kind: KeyPaste, Text: string(d.pasteBuf)})
				d.pasteBuf = d.pasteBuf[:0]
				d.pasting = false
				continue
			}
			keep := suffixPrefixLen(d.pending, pasteEnd)
			d.pasteBuf = append(d.pasteBuf, d.pending[:len(d.pending)-keep]...)
			d.pending = d.pending[len(d.pending)-keep:]
			break
		}

		if d.pending[0] == 0x1b {
			flushText()
			sequence, kind, complete := decodeEscape(d.pending)
			if !complete {
				break
			}
			d.pending = d.pending[sequence:]
			if kind == KeyPaste {
				d.pasting = true
				continue
			}
			if kind != 0 {
				keys = append(keys, Key{Kind: kind})
			}
			continue
		}

		kind := KeyKind(0)
		switch d.pending[0] {
		case '\r', '\n':
			kind = KeyEnter
		case 0x03:
			kind = KeyInterrupt
		case 0x04:
			kind = KeyEOF
		case 0x08, 0x7f:
			kind = KeyBackspace
		case '\t':
			kind = KeyTab
		}
		if kind != 0 {
			flushText()
			keys = append(keys, Key{Kind: kind})
			d.pending = d.pending[1:]
			continue
		}

		if !utf8.FullRune(d.pending) {
			break
		}
		r, size := utf8.DecodeRune(d.pending)
		text.WriteRune(r)
		d.pending = d.pending[size:]
	}
	flushText()
	return keys
}

func decodeEscape(input []byte) (consumed int, kind KeyKind, complete bool) {
	sequences := []struct {
		bytes []byte
		kind  KeyKind
	}{
		{pasteStart, KeyPaste},
		{[]byte("\x1b[13;2u"), KeyNewline},
		{[]byte("\x1b\r"), KeyNewline},
		{[]byte("\x1b\n"), KeyNewline},
		{[]byte("\x1b[A"), KeyUp},
		{[]byte("\x1b[B"), KeyDown},
		{[]byte("\x1b[C"), KeyRight},
		{[]byte("\x1b[D"), KeyLeft},
		{[]byte("\x1b[H"), KeyHome},
		{[]byte("\x1b[F"), KeyEnd},
		{[]byte("\x1b[3~"), KeyDelete},
		// Shift+Tab. Without this it is swallowed by the unknown-CSI branch
		// below, which is indistinguishable from the key doing nothing.
		{[]byte("\x1b[Z"), KeyShiftTab},
		{[]byte("\x1b[5~"), KeyPageUp},
		{[]byte("\x1b[6~"), KeyPageDown},
		// Wheel up/down in SGR mouse mode. Decoded so a wheel scrolls the
		// suggestion list where the terminal already reports it; kolk does not
		// turn mouse reporting ON, because doing so takes click-drag text
		// selection away from the terminal, and losing copy-paste to gain a
		// scroll is a bad trade. A terminal configured to send these — or one
		// where the user holds the modifier their emulator uses — is honoured.
		{[]byte("\x1b[<64;"), KeyPageUp},
		{[]byte("\x1b[<65;"), KeyPageDown},
	}
	for _, sequence := range sequences {
		if bytes.HasPrefix(input, sequence.bytes) {
			return len(sequence.bytes), sequence.kind, true
		}
		if bytes.HasPrefix(sequence.bytes, input) {
			return 0, 0, false
		}
	}
	// Consume one complete unknown CSI/SS3 sequence so an unsupported key
	// never leaks its bytes into the user's prompt.
	if len(input) >= 2 && (input[1] == '[' || input[1] == 'O') {
		for i := 2; i < len(input); i++ {
			if input[i] >= 0x40 && input[i] <= 0x7e {
				return i + 1, 0, true
			}
		}
		return 0, 0, false
	}
	if len(input) == 1 {
		return 0, 0, false
	}
	return 2, 0, true
}

func suffixPrefixLen(input, marker []byte) int {
	limit := min(len(input), len(marker)-1)
	for n := limit; n > 0; n-- {
		if bytes.Equal(input[len(input)-n:], marker[:n]) {
			return n
		}
	}
	return 0
}
