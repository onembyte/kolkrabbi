// Package tui owns Kolkrabbi's interactive terminal screen model. It contains
// no engine or terminal I/O: adapters feed it transcript, activity, status,
// and draft changes as independent regions.
package tui

import (
	"fmt"
	"strings"
	"unicode"
)

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
	Transcript string
	Activity   string
	Draft      string
	Status     Status
}

// Model contains logical screen state only. One terminal event loop owns a
// Model, so synchronization belongs at that boundary rather than inside every
// field mutation.
type Model struct {
	transcript strings.Builder
	activity   string
	draft      string
	status     Status
}

// New returns an empty screen with the supplied session state.
func New(status Status) *Model { return &Model{status: status} }

// AppendTranscript adds model or tool output without touching activity or the
// current input draft.
func (m *Model) AppendTranscript(chunk string) { _, _ = m.transcript.WriteString(chunk) }

// SetActivity replaces the ephemeral lifecycle line without writing it into
// scrollback.
func (m *Model) SetActivity(activity string) { m.activity = activity }

// SetDraft replaces the composer contents exactly. Leading/trailing space and
// newlines are meaningful input and are deliberately not normalized.
func (m *Model) SetDraft(draft string) { m.draft = draft }

// SetStatus atomically replaces the compact state row.
func (m *Model) SetStatus(status Status) { m.status = status }

// Snapshot returns independent values rather than aliases into mutable state.
func (m *Model) Snapshot() Snapshot {
	return Snapshot{
		Transcript: m.transcript.String(),
		Activity:   m.activity,
		Draft:      m.draft,
		Status:     m.status,
	}
}

// View renders the logical region order. Transcript rows yield space first;
// activity and compact status yield next; the composer is always the final
// region. Visual wrapping never changes the stored draft.
func (m *Model) View(width, height int) string {
	if width < 4 {
		width = 4
	}

	composer := m.composerLines(width)
	activity := []string{}
	if m.activity != "" {
		activity = []string{clipLine(m.activity, width)}
	}
	statusLine := []string{}
	if status := formatStatus(m.status); status != "" {
		statusLine = []string{clipLine(status, width)}
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

	transcript := wrapText(m.transcript.String(), width)
	if height > 0 {
		available := height - len(activity) - len(statusLine) - len(composer)
		if available <= 0 {
			transcript = nil
		} else if len(transcript) > available {
			transcript = transcript[len(transcript)-available:]
		}
	}

	lines := make([]string, 0, len(transcript)+len(activity)+len(statusLine)+len(composer))
	lines = append(lines, transcript...)
	lines = append(lines, activity...)
	lines = append(lines, statusLine...)
	lines = append(lines, composer...)
	return strings.Join(lines, "\n")
}

func (m *Model) composerLines(width int) []string {
	prompt := "kolk"
	if m.status.Mode != "" {
		prompt += "-" + m.status.Mode
	}
	lines := []string{clipLine(fmt.Sprintf("╭─ %s", prompt), width)}
	contentWidth := max(1, width-2)
	for _, line := range strings.Split(m.draft, "\n") {
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
		if value != "" {
			visible = append(visible, value)
		}
	}
	return strings.Join(visible, " · ")
}
