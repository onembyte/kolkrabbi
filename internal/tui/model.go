// Package tui owns Kolkrabbi's interactive terminal screen model. It contains
// no engine or terminal I/O: adapters feed it transcript, activity, status,
// and draft changes as independent regions.
package tui

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxTranscriptBytes = 4 * 1024 * 1024

// Status is the compact state row shown between the transcript and composer.
// Values are already user-facing labels; the model never resolves product
// policy or reaches into the engine.
type Status struct {
	Model     string
	Mode      string
	Effort    string
	Session   string
	Approval  string
	Lifecycle string
}

// Snapshot is an immutable copy of the screen regions. Tests and future
// protocol frontends use it to prove one region cannot corrupt another.
type Snapshot struct {
	Transcript  string
	Activity    string
	Draft       string
	Status      Status
	Suggestions []CommandSpec
}

// Model contains logical screen state only. One terminal event loop owns a
// Model, so synchronization belongs at that boundary rather than inside every
// field mutation.
type Model struct {
	transcript  []byte
	activity    string
	draft       string
	status      Status
	suggestions []CommandSpec
	selected    int
}

// New returns an empty screen with the supplied session state.
func New(status Status) *Model { return &Model{status: status, selected: -1} }

// AppendTranscript adds model or tool output without touching activity or the
// current input draft.
func (m *Model) AppendTranscript(chunk string) {
	m.transcript = appendTranscriptBounded(m.transcript, sanitizeTerminalText(chunk), maxTranscriptBytes)
}

// SetActivity replaces the ephemeral lifecycle line without writing it into
// scrollback.
func (m *Model) SetActivity(activity string) { m.activity = sanitizeTerminalLine(activity) }

// SetDraft replaces the composer contents exactly. Leading/trailing space and
// newlines are meaningful input and are deliberately not normalized.
func (m *Model) SetDraft(draft string) { m.draft = draft }

// SetStatus atomically replaces the compact state row.
func (m *Model) SetStatus(status Status) { m.status = status }

// SetSuggestions replaces the ephemeral slash-command menu.
func (m *Model) SetSuggestions(suggestions []CommandSpec) {
	m.suggestions = append(m.suggestions[:0], suggestions...)
	m.selected = -1
}

// SetSuggestionSelection marks one ephemeral command-menu row.
func (m *Model) SetSuggestionSelection(selected int) {
	if selected < 0 || selected >= len(m.suggestions) {
		m.selected = -1
		return
	}
	m.selected = selected
}

// Snapshot returns independent values rather than aliases into mutable state.
func (m *Model) Snapshot() Snapshot {
	return Snapshot{
		Transcript:  string(m.transcript),
		Activity:    m.activity,
		Draft:       m.draft,
		Status:      m.status,
		Suggestions: append([]CommandSpec(nil), m.suggestions...),
	}
}

// View renders the logical region order. Transcript rows yield space first;
// activity and compact status yield next; the composer is always the final
// region. Visual wrapping never changes the stored draft.
func (m *Model) View(width, height int) string {
	return m.view(width, height, -1)
}

func (m *Model) view(width, height, cursor int) string {
	if width < 4 {
		width = 4
	}

	composer := m.composerLines(width, cursor)
	activity := []string{}
	if m.activity != "" {
		activity = []string{clipLine(m.activity, width)}
	}
	statusLine := []string{}
	if status := formatStatus(m.status); status != "" {
		statusLine = []string{clipLine(status, width)}
	}
	suggestions := make([]string, 0, len(m.suggestions))
	for index, suggestion := range m.suggestions {
		marker := "  "
		if index == m.selected {
			marker = "› "
		}
		line := marker + sanitizeTerminalLine(suggestion.Usage)
		if suggestion.Summary != "" {
			line += "  " + sanitizeTerminalLine(suggestion.Summary)
		}
		suggestions = append(suggestions, clipLine(line, width))
	}

	// An exceptionally short terminal keeps the input tail and its closing
	// boundary. The full draft remains in the model for resize or submission.
	if height > 0 && len(composer) > height {
		composer = composer[len(composer)-height:]
	}
	for height > 0 && len(activity)+len(statusLine)+len(composer) > height && len(activity) > 0 {
		activity = nil
	}
	for height > 0 && len(statusLine)+len(composer) > height && len(statusLine) > 0 {
		statusLine = nil
	}
	if height > 0 {
		available := max(0, height-len(activity)-len(statusLine)-len(composer))
		if len(suggestions) > available {
			suggestions = suggestions[:available]
		}
	}

	transcript := wrapText(string(m.transcript), width)
	if height > 0 {
		available := height - len(activity) - len(statusLine) - len(suggestions) - len(composer)
		if available <= 0 {
			transcript = nil
		} else if len(transcript) > available {
			transcript = transcript[len(transcript)-available:]
		}
	}

	lines := make([]string, 0, len(transcript)+len(activity)+len(statusLine)+len(suggestions)+len(composer))
	lines = append(lines, transcript...)
	lines = append(lines, activity...)
	lines = append(lines, statusLine...)
	lines = append(lines, suggestions...)
	lines = append(lines, composer...)
	return strings.Join(lines, "\n")
}

func (m *Model) composerLines(width, cursor int) []string {
	prompt := "kolk"
	if m.status.Mode != "" {
		prompt += "-" + sanitizeTerminalLine(m.status.Mode)
	}
	lines := []string{clipLine(fmt.Sprintf("╭─ %s", prompt), width)}
	contentWidth := max(1, width-2)
	draft := m.draft
	if cursor >= 0 {
		runes := []rune(draft)
		cursor = min(cursor, len(runes))
		runes = append(runes, 0)
		copy(runes[cursor+1:], runes[cursor:len(runes)-1])
		runes[cursor] = '▌'
		draft = string(runes)
	}
	draft = sanitizeTerminalText(draft)
	for _, line := range strings.Split(draft, "\n") {
		for _, wrapped := range wrapLine(line, contentWidth) {
			lines = append(lines, "│ "+wrapped)
		}
	}
	return append(lines, clipLine("╰─", width))
}

func wrapText(text string, width int) []string {
	if text == "" {
		return nil
	}
	logical := strings.Split(text, "\n")
	if logical[len(logical)-1] == "" {
		logical = logical[:len(logical)-1]
	}
	var lines []string
	for _, line := range logical {
		lines = append(lines, wrapLine(line, width)...)
	}
	return lines
}

func wrapLine(line string, width int) []string {
	if line == "" {
		return []string{""}
	}
	current := make([]rune, 0, len(line))
	used := 0
	var lines []string
	for _, r := range line {
		cells := runeCellWidth(r)
		if used+cells > width && len(current) > 0 {
			lines = append(lines, string(current))
			current = current[:0]
			used = 0
		}
		current = append(current, r)
		used += cells
	}
	return append(lines, string(current))
}

func clipLine(line string, width int) string {
	if cellWidth(line) <= width {
		return line
	}
	if width == 1 {
		return "…"
	}
	return wrapLine(line, width-1)[0] + "…"
}

func cellWidth(text string) int {
	width := 0
	for _, r := range text {
		width += runeCellWidth(r)
	}
	return width
}

// runeCellWidth covers the width rules needed by terminals without adding a
// third Unicode dependency to the root graph. Combining/format code points
// occupy no cell; the East Asian and emoji ranges occupy two.
func runeCellWidth(r rune) int {
	if r == 0 || r == '\u200d' || unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) ||
		unicode.Is(unicode.Cf, r) {
		return 0
	}
	if isWideRune(r) {
		return 2
	}
	return 1
}

func isWideRune(r rune) bool {
	return r >= 0x1100 && (r <= 0x115f ||
		r == 0x2329 || r == 0x232a ||
		(r >= 0x2e80 && r <= 0xa4cf && r != 0x303f) ||
		(r >= 0xac00 && r <= 0xd7a3) ||
		(r >= 0xf900 && r <= 0xfaff) ||
		(r >= 0xfe10 && r <= 0xfe19) ||
		(r >= 0xfe30 && r <= 0xfe6f) ||
		(r >= 0xff00 && r <= 0xff60) ||
		(r >= 0xffe0 && r <= 0xffe6) ||
		(r >= 0x1f300 && r <= 0x1faff) ||
		(r >= 0x20000 && r <= 0x3fffd))
}

func formatStatus(status Status) string {
	values := []string{
		status.Model,
		status.Mode,
		status.Effort,
		status.Session,
		status.Approval,
		status.Lifecycle,
	}
	visible := values[:0]
	for _, value := range values {
		value = sanitizeTerminalLine(value)
		if value != "" {
			visible = append(visible, value)
		}
	}
	return strings.Join(visible, " · ")
}

func appendTranscriptBounded(transcript []byte, chunk string, limit int) []byte {
	if limit <= 0 {
		return transcript[:0]
	}
	transcript = append(transcript, chunk...)
	if len(transcript) <= limit {
		return transcript
	}
	start := len(transcript) - limit
	for start < len(transcript) && !utf8.RuneStart(transcript[start]) {
		start++
	}
	copy(transcript, transcript[start:])
	return transcript[:len(transcript)-start]
}

// sanitizeTerminalText removes cursor-addressing and C0 controls from model,
// tool, provider, and pasted text before it reaches the renderer. Newlines are
// retained as content; tabs become stable spaces so terminal tab stops cannot
// invalidate width accounting.
func sanitizeTerminalText(text string) string {
	var safe strings.Builder
	safe.Grow(len(text))
	for index := 0; index < len(text); {
		if text[index] == 0x1b {
			index = skipEscapeSequence(text, index)
			continue
		}
		switch text[index] {
		case '\n':
			safe.WriteByte('\n')
			index++
			continue
		case '\t':
			safe.WriteString("    ")
			index++
			continue
		}
		if text[index] < 0x20 || text[index] == 0x7f {
			index++
			continue
		}
		r, size := utf8.DecodeRuneInString(text[index:])
		if r == utf8.RuneError && size == 1 {
			safe.WriteRune(utf8.RuneError)
			index++
			continue
		}
		safe.WriteRune(r)
		index += size
	}
	return safe.String()
}

func sanitizeTerminalLine(text string) string {
	return strings.ReplaceAll(sanitizeTerminalText(text), "\n", " ")
}

func skipEscapeSequence(text string, start int) int {
	next := start + 1
	if next >= len(text) {
		return next
	}
	switch text[next] {
	case '[': // CSI: parameters/intermediates followed by one final byte.
		next++
		for next < len(text) {
			final := text[next]
			next++
			if final >= 0x40 && final <= 0x7e {
				break
			}
		}
		return next
	case ']': // OSC: terminated by BEL or ST (ESC backslash).
		next++
		for next < len(text) {
			if text[next] == 0x07 {
				return next + 1
			}
			if text[next] == 0x1b && next+1 < len(text) && text[next+1] == '\\' {
				return next + 2
			}
			next++
		}
		return next
	default:
		for next < len(text) && text[next] >= 0x20 && text[next] <= 0x2f {
			next++
		}
		if next < len(text) {
			next++
		}
		return next
	}
}
