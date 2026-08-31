// Package tui owns Kolkrabbi's interactive terminal screen model. It contains
// no engine or terminal I/O: adapters feed it transcript, activity, status,
// and draft changes as independent regions.
package tui

import (
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

const maxTranscriptBytes = 4 * 1024 * 1024

// Status is the compact state row shown between the transcript and composer.
// Values are already user-facing labels; the model never resolves product
// policy or reaches into the engine.
type Status struct {
	Model       string
	Mode        string
	Effort      string
	Session     string
	SessionName string
	Folder      string
	Approval    string
	Lifecycle   string
	// Context and Cost are the two numbers that decide whether to compact or
	// stop. Empty means not measured yet, which is different from zero.
	Context string
	Cost    string
	// Agents is how many subagents are running right now. Zero shows nothing:
	// a permanent "agents 0" on every session is the sort of always-there
	// number people stop reading, and this one is worth reading.
	Agents int
	// Queued is requests typed and held while a turn runs. It answers the one
	// question the spinner cannot: "will what I just typed actually send?"
	Queued int
}

// Snapshot is an immutable copy of the screen regions. Tests and future
// protocol frontends use it to prove one region cannot corrupt another.
type Snapshot struct {
	Transcript    string
	Activity      string
	Draft         string
	Status        Status
	AgentStatuses []AgentStatus
	Suggestions   []CommandSpec
}

// Model contains logical screen state only. One terminal event loop owns a
// Model, so synchronization belongs at that boundary rather than inside every
// field mutation.
type Model struct {
	transcript       []byte
	activity         string
	draft            string
	status           Status
	agentStatuses    []AgentStatus
	suggestions      []CommandSpec
	suggestionTop    int
	suggestionWindow int
	suggestionTotal  int
	selected         int
}

// New returns an empty screen with the supplied session state.
func New(status Status) *Model { return &Model{status: status, selected: -1} }

// AppendTranscript adds model or tool output without touching activity or the
// current input draft.
func (m *Model) AppendTranscript(chunk string) {
	m.transcript = appendTranscriptBounded(m.transcript, sanitizeTerminalText(chunk), maxTranscriptBytes)
}

// SetActivity replaces the ephemeral lifecycle region without writing it into
// scrollback. Newlines allow small multi-row status sprites.
func (m *Model) SetActivity(activity string) { m.activity = sanitizeTerminalText(activity) }

// SetDraft replaces the composer contents exactly. Leading/trailing space and
// newlines are meaningful input and are deliberately not normalized.
func (m *Model) SetDraft(draft string) { m.draft = draft }

// SetStatus atomically replaces the compact state row.
func (m *Model) SetStatus(status Status) { m.status = status }

// SetAgentStatuses replaces the ephemeral per-task rows without touching the
// spinner activity or transcript.
func (m *Model) SetAgentStatuses(statuses []AgentStatus) {
	m.agentStatuses = append(m.agentStatuses[:0], statuses...)
}

// SetSuggestions replaces the ephemeral slash-command menu.
func (m *Model) SetSuggestions(suggestions []CommandSpec) {
	m.suggestions = append(m.suggestions[:0], suggestions...)
	m.selected = -1
	m.suggestionTop, m.suggestionWindow = 0, 0
}

// SetSuggestionWindow scrolls the list: top is the first row to draw, window
// how many fit, and total how many there are so the footer can say what is
// off screen. A list that shows eight of thirty-five without saying so reads
// as a list of eight.
func (m *Model) SetSuggestionWindow(top, window, total int) {
	m.suggestionTop, m.suggestionWindow, m.suggestionTotal = top, window, total
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
		Transcript:    string(m.transcript),
		Activity:      m.activity,
		Draft:         m.draft,
		Status:        m.status,
		AgentStatuses: append([]AgentStatus(nil), m.agentStatuses...),
		Suggestions:   append([]CommandSpec(nil), m.suggestions...),
	}
}

// View renders the logical region order. Transcript rows yield space first;
// activity and suggestions yield next; the framed composer and its compact
// status footer remain persistent. Visual wrapping never changes the draft.
func (m *Model) View(width, height int) string {
	return m.view(width, height, -1)
}

func (m *Model) view(width, height, cursor int) string {
	return joinViewRowsWidth(m.viewRows(width, height, cursor), false, width)
}

func (m *Model) renderView(width, height, cursor int) string {
	return joinViewRowsWidth(m.viewRows(width, height, cursor), true, width)
}

type rowStyle uint8

// rowStyle is the vocabulary the whole screen draws with. The transcript is
// sanitized on ingest, so these fixed styles are the only colour in the app —
// they attach to structural facts (this row is a heading, a diff line, meta)
// and never to content the model chose. A palette maps each style to the
// escape sequences of one terminal tier; three tiers exist because the
// alternatives each fail differently: a 16-color-only terminal given
// 256-color sequences shows garbage, and a NO_COLOR user who asked out of
// colour gets none of it.
const (
	styleNone rowStyle = iota
	stylePurple
	stylePurpleMuted
	styleHeading
	styleMeta
	styleAdd
	styleDel
	styleWarn
)

const (
	// productName sits in the composer's top rule.
	productName = "kolkrabbi"
	// promptMarker opens the draft. statusIndent aligns the footer under it.
	promptMarker = "❯"
	statusIndent = "  "
)

// palette maps one rowStyle to the escape sequences of one terminal tier.
type palette map[rowStyle]string

var palette256 = palette{
	// styleHeading is one compound SGR, not two stacked opens: every style
	// here is paired with exactly one reset downstream, and an audit that
	// counts opens against resets would (correctly) call two opens a leak.
	stylePurple:      "\x1b[38;5;141m",
	stylePurpleMuted: "\x1b[38;5;103m",
	styleHeading:     "\x1b[38;5;141;1m",
	styleMeta:        "\x1b[2m",
	styleAdd:         "\x1b[38;5;114m",
	styleDel:         "\x1b[38;5;174m",
	styleWarn:        "\x1b[38;5;221m",
}

var palette16 = palette{
	stylePurple:      "\x1b[95m",
	stylePurpleMuted: "\x1b[90m",
	styleHeading:     "\x1b[95;1m",
	styleMeta:        "\x1b[2m",
	styleAdd:         "\x1b[32m",
	styleDel:         "\x1b[31m",
	styleWarn:        "\x1b[33m",
}

// activePalette is process state, not screen state: the terminal's colour
// capability cannot change while kolk is attached to it, and every render
// reads it. The CLI picks the tier once at startup, honouring NO_COLOR.
var (
	paletteMu     sync.RWMutex
	activePalette = palette256
)

// SetPalette selects the escape tier: "256", "16", or "none". It is called
// once by the CLI before the first frame, from the same capability probe the
// legacy line REPL uses for its colour.
func SetPalette(tier string) {
	paletteMu.Lock()
	defer paletteMu.Unlock()
	switch tier {
	case "16":
		activePalette = palette16
	case "none":
		activePalette = palette{styleNone: ""}
	default:
		activePalette = palette256
	}
}

const resetANSI = "\x1b[0m"

type viewRow struct {
	text  string
	style rowStyle
	// right is drawn flush with the right edge of the row, in its own style.
	// The activity indicator lives there: it belongs beside the state it
	// describes, not on a row of its own above the composer, and it must not
	// take the muted styling the status fields use.
	right      string
	rightStyle rowStyle
}

func (m *Model) viewRows(width, height, cursor int) []viewRow {
	rows, _ := m.layout(width, height, cursor)
	return rows
}

// layout builds the frame and reports how many rows are left for transcript.
// Both answers come from one pass so that what gets committed to scrollback and
// what stays on screen can never disagree about where the fold is.
func (m *Model) layout(width, height, cursor int) ([]viewRow, int) {
	if width < 4 {
		width = 4
	}

	composerText := m.composerLines(width, cursor)
	composer := make([]viewRow, len(composerText))
	for index, line := range composerText {
		style := styleNone
		if index == 0 || index == len(composerText)-1 {
			style = stylePurple
		}
		composer[index] = viewRow{text: line, style: style}
	}
	activity := []viewRow{}
	if m.activity != "" {
		for _, line := range strings.Split(m.activity, "\n") {
			activity = append(activity, viewRow{text: clipLine(line, width), style: stylePurple})
		}
	}
	agentRows := make([]viewRow, 0, len(m.agentStatuses))
	for _, status := range m.agentStatuses {
		agentRows = append(agentRows, viewRow{
			text: clipLine(formatAgentStatusLine(status), width), style: agentStatusStyle(status),
		})
	}
	statusLine := []viewRow{}
	for _, status := range formatStatus(m.status) {
		statusLine = append(statusLine, viewRow{text: clipLine(status, width), style: stylePurpleMuted})
	}
	// The indicator sits at the right end of the first status row: beside the
	// state it describes, below the composer, rather than on a row of its own
	// above it that pushed the whole screen down whenever a turn started.
	// Multi-line activity, and a terminal too narrow to share the row, keep the
	// old placement.
	if len(statusLine) > 0 && len(activity) == 1 &&
		cellWidth(statusLine[0].text)+cellWidth(activity[0].text)+1 <= width {
		statusLine[0].right = activity[0].text
		statusLine[0].rightStyle = stylePurple
		activity = nil
	}
	// Only the window is drawn. The selection may sit anywhere in the full
	// list; the controller keeps top such that it is inside this slice.
	first, last := 0, len(m.suggestions)
	if m.suggestionWindow > 0 {
		first = min(max(0, m.suggestionTop), max(0, len(m.suggestions)-1))
		last = min(len(m.suggestions), first+m.suggestionWindow)
	}
	suggestions := make([]viewRow, 0, last-first+2)
	// The same arrow, pointing the other way. Scrolled down, the rows above are
	// as invisible as the ones below were, and the reader has no way to know
	// the list did not start here.
	if first > 0 {
		suggestions = append(suggestions, viewRow{text: clipLine("  ↑", width), style: stylePurpleMuted})
	}
	for index := first; index < last; index++ {
		suggestion := m.suggestions[index]
		marker := "  "
		style := stylePurpleMuted
		if index == m.selected {
			marker = "> "
			style = stylePurple
		}
		line := marker + sanitizeTerminalLine(suggestion.Usage)
		if suggestion.Summary != "" {
			line += "  " + sanitizeTerminalLine(suggestion.Summary)
		}
		suggestions = append(suggestions, viewRow{text: clipLine(line, width), style: style})
	}
	// One arrow, and only while there is something below it. A count and a
	// key hint are a legend for a list that does not need one; the arrow says
	// the only thing the reader cannot already see, and disappears the moment
	// it stops being true.
	if last < len(m.suggestions) {
		suggestions = append(suggestions, viewRow{text: clipLine("  ↓", width), style: stylePurpleMuted})
	}

	// An exceptionally short terminal keeps the input tail and its closing
	// boundary. The full draft remains in the model for resize or submission.
	if height > 0 && len(composer) > height {
		composer = composer[len(composer)-height:]
	}
	for height > 0 && len(activity)+len(agentRows)+len(statusLine)+len(composer) > height && len(activity) > 0 {
		activity = nil
	}
	for height > 0 && len(agentRows)+len(statusLine)+len(composer) > height && len(agentRows) > 0 {
		agentRows = agentRows[1:]
	}
	for height > 0 && len(statusLine)+len(composer) > height && len(statusLine) > 0 {
		statusLine = nil
	}
	if height > 0 {
		available := max(0, height-len(activity)-len(agentRows)-len(statusLine)-len(composer))
		if len(suggestions) > available {
			suggestions = suggestions[:available]
		}
	}

	transcriptRows := renderMarkdownStyled(string(m.transcript), width)
	// Negative means unbounded: a caller that passed no height wants the whole
	// transcript, and nothing should be committed out from under it.
	budget := -1
	if height > 0 {
		available := height - len(activity) - len(agentRows) - len(statusLine) - len(suggestions) - len(composer)
		budget = max(0, available)
		if available <= 0 {
			transcriptRows = nil
		} else if len(transcriptRows) > available {
			transcriptRows = transcriptRows[len(transcriptRows)-available:]
		} else if len(transcriptRows) < available {
			// Pad above, so the frame is always exactly the height of the
			// terminal and the composer is always on its last row.
			//
			// Without this the frame was only as tall as its content, which
			// meant the composer sat near the top of an empty session and
			// dropped to the bottom the moment enough output arrived to fill
			// the screen -- and on a resize it appeared to jump upward, because
			// the terminal adds its new rows below a frame that is not anchored
			// to anything. One height, one position, from the first frame on.
			padded := make([]styledRow, available-len(transcriptRows), available)
			transcriptRows = append(padded, transcriptRows...)
		}
	}

	rows := make([]viewRow, 0, len(transcriptRows)+len(activity)+len(agentRows)+len(statusLine)+len(suggestions)+len(composer))
	for _, row := range transcriptRows {
		// A request the user sent is theirs, and reads as theirs: the same
		// purple the composer uses, so the eye can find "what did I ask" in a
		// long transcript without reading it. The renderer's own styles stand
		// for everything that was not typed by the user.
		if transcriptStyle(row.text) == stylePurple {
			row.style = stylePurple
		}
		rows = append(rows, viewRow{text: row.text, style: row.style})
	}
	rows = append(rows, activity...)
	rows = append(rows, agentRows...)
	rows = append(rows, suggestions...)
	rows = append(rows, composer...)
	rows = append(rows, statusLine...)
	return rows, budget
}

// agentStatusStyle colours structural state, never model-provided text. The
// visible state word remains in the row, so NO_COLOR loses decoration rather
// than meaning.
func agentStatusStyle(status AgentStatus) rowStyle {
	switch status.State {
	case "working":
		return stylePurple
	case "done":
		return styleAdd
	case "failed":
		return styleDel
	case "waiting", "blocked":
		return styleWarn
	default: // queued and legacy/unknown producers stay deliberately quiet.
		return stylePurpleMuted
	}
}

// CommitOverflow removes the transcript that no longer fits on screen and
// returns it, so the caller can hand it to the terminal's scrollback.
//
// Without this the frame is repainted in place: every new line shifts the
// others up a row and the top one is overwritten. That is why agent mode, which
// produces far more output than a chat reply, looked like it was "printing
// upwards" -- and why none of what scrolled past could be read afterwards.
//
// The cut is only ever made at a block boundary, where rendering the part that
// leaves produces exactly the lines that were already on screen.
func (m *Model) CommitOverflow(width, height int) []viewRow {
	if width < 4 {
		width = 4
	}
	_, budget := m.layout(width, height, -1)
	if budget < 0 {
		return nil
	}
	rendered, boundaries := renderMarkdownStyledBlocks(string(m.transcript), width)
	if len(rendered) <= budget {
		return nil
	}

	// Take the most that fits entirely above the fold. Committing a line that
	// is still visible would print it twice.
	overflow := len(rendered) - budget
	cut := blockBoundary{}
	for _, boundary := range boundaries {
		if boundary.source > 0 && boundary.rendered <= overflow {
			cut = boundary
		}
	}
	if cut.source == 0 {
		// One block taller than the screen -- a long code fence, say. It has to
		// stay whole, so it is clipped as before rather than cut in half.
		return nil
	}

	offset := offsetAfterLines(m.transcript, cut.source)
	committed := make([]viewRow, 0, cut.rendered)
	for _, row := range rendered[:cut.rendered] {
		if transcriptStyle(row.text) == stylePurple {
			row.style = stylePurple
		}
		committed = append(committed, viewRow{text: row.text, style: row.style})
	}
	m.transcript = m.transcript[:copy(m.transcript, m.transcript[offset:])]
	return committed
}

// offsetAfterLines is the byte index just past the count-th newline. The
// transcript is sanitized on the way in, so its newlines are exactly the ones
// the renderer split on.
func offsetAfterLines(transcript []byte, count int) int {
	for index := 0; index < len(transcript); index++ {
		if transcript[index] != '\n' {
			continue
		}
		count--
		if count == 0 {
			return index + 1
		}
	}
	return len(transcript)
}

// transcriptStyle marks a line the user typed. It is the composer's own purple,
// so a request reads as theirs whether it is on screen or in scrollback.
func transcriptStyle(line string) rowStyle {
	if strings.HasPrefix(line, promptMarker+" ") {
		return stylePurple
	}
	return styleNone
}

func joinViewRows(rows []viewRow, styled bool) string {
	return joinViewRowsWidth(rows, styled, 0)
}

// joinViewRowsWidth renders rows, placing any right-aligned field flush with
// width. It composes here rather than earlier because a right field carries its
// own style, and padding has to be measured on visible runes, not escape bytes.
func joinViewRowsWidth(rows []viewRow, styled bool, width int) string {
	var output strings.Builder
	for index, row := range rows {
		if index > 0 {
			output.WriteByte('\n')
		}
		pad := ""
		if row.right != "" && width > 0 {
			// Cells, not runes: a CJK session title or emoji in the activity
			// line takes two columns per glyph, and a rune-count gap under-pads
			// the row so the frame hard-wraps mid-write.
			gap := width - cellWidth(row.text) - cellWidth(row.right)
			if gap >= 1 {
				pad = strings.Repeat(" ", gap)
			} else {
				// Too narrow to share the row; the caller keeps it on its own.
				row.right = ""
			}
		}
		writeStyled(&output, row.text, row.style, styled)
		if row.right != "" {
			output.WriteString(pad)
			writeStyled(&output, row.right, row.rightStyle, styled)
		}
	}
	return output.String()
}

func writeStyled(output *strings.Builder, text string, style rowStyle, styled bool) {
	if text == "" {
		return
	}
	if !styled || style == styleNone {
		output.WriteString(text)
		return
	}
	paletteMu.RLock()
	sequence := activePalette[style]
	paletteMu.RUnlock()
	if sequence == "" {
		output.WriteString(text)
		return
	}
	output.WriteString(sequence)
	output.WriteString(text)
	output.WriteString(resetANSI)
}

func (m *Model) composerLines(width, cursor int) []string {
	// The top rule carries the mode and the product name at its right end; the
	// closing rule stays unbroken, so the frame still reads as a frame.
	lines := []string{composerTopRule(m.status.Mode, width)}
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
	first := true
	for _, line := range strings.Split(draft, "\n") {
		for _, wrapped := range wrapLine(line, contentWidth) {
			prefix := "  "
			if first {
				prefix = promptMarker + " "
				first = false
			}
			lines = append(lines, prefix+wrapped)
		}
	}
	return append(lines, strings.Repeat("─", width))
}

// composerTopRule draws the composer's opening rule with the mode and the
// product name set into its right end:
//
//	──────────────────────── code ──── kolkrabbi ─
//
// Right-aligned rather than centred: the eye reads the draft from the left, so
// a label there sits in front of the text, while the right end of the rule is
// empty space the frame was spending anyway. A terminal too narrow for the
// label keeps the plain rule instead of clipping the name to nonsense.
func composerTopRule(mode string, width int) string {
	plain := strings.Repeat("─", max(0, width))
	mode = sanitizeTerminalLine(strings.TrimSpace(mode))
	if mode == "" {
		return plain
	}
	label := " " + mode + " ──── " + productName + " "
	// One dash of rule after the name, and at least four leading it, or the
	// label stops reading as something set into a line.
	if width < cellWidth(label)+5 {
		return plain
	}
	return strings.Repeat("─", width-cellWidth(label)-1) + label + "─"
}

func horizontalRule(label string, width int) string {
	if width < 5 {
		return strings.Repeat("─", max(0, width))
	}
	label = clipLine(sanitizeTerminalLine(label), width-4)
	title := " " + label + " "
	remaining := width - cellWidth(title)
	left := max(1, remaining/2)
	right := max(1, remaining-left)
	return strings.Repeat("─", left) + title + strings.Repeat("─", right)
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

// wrapWords wraps at the last space that fits, breaking a word only when no
// space fits on the line at all. Prose that splits mid-word reads as broken
// text; code and input rows keep the exact character wrap of wrapLine.
func wrapWords(line string, width int) []string {
	if line == "" {
		return []string{""}
	}
	if cellWidth(line) <= width {
		return []string{line}
	}
	var out []string
	for line != "" {
		// The last space whose column still fits inside the width…
		spaceAt := -1
		used := 0
		for index, r := range line {
			cells := runeCellWidth(r)
			if used+cells > width {
				break
			}
			used += cells
			if r == ' ' {
				spaceAt = index
			}
		}
		if spaceAt >= 0 {
			// A run of spaces wrapping across the boundary is not a row of its
			// own: emitting the trimmed head only when it says something keeps
			// the wrap from opening with a phantom blank line.
			if head := strings.TrimRight(line[:spaceAt], " "); head != "" {
				out = append(out, head)
			}
			line = strings.TrimLeft(line[spaceAt:], " ")
			continue
		}
		// …otherwise hard-break. A first rune wider than the whole row
		// would otherwise loop forever on its own.
		hard := 0
		used = 0
		for index, r := range line {
			cells := runeCellWidth(r)
			if used+cells > width {
				break
			}
			used += cells
			hard = index + utf8.RuneLen(r)
		}
		if hard == 0 {
			hard = utf8.RuneLen(firstRune(line))
		}
		out = append(out, line[:hard])
		line = strings.TrimLeft(line[hard:], " ")
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

func firstRune(text string) rune {
	for _, r := range text {
		return r
	}
	return 0
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

func formatStatus(status Status) []string {
	sessionLabel := status.SessionName
	if sessionLabel == "" {
		sessionLabel = status.Session
	}
	type statusField struct {
		label string
		value string
	}
	groups := [][]statusField{
		{
			{label: "mode", value: status.Mode},
			{label: "effort", value: status.Effort},
			// Last in this group, so a narrow terminal clips these before the
			// mode or the tier.
			{label: "folder", value: status.Folder},
			{label: "state", value: status.Lifecycle},
			// What the run is doing belongs on the row that already carries
			// mode and state, not beside the cost.
			{label: "agents", value: agentCount(status.Agents)},
			// A queued request that only its author can see is a dropped one
			// as far as everyone else is concerned.
			{label: "queued", value: queuedCount(status.Queued)},
		},
		{
			{label: "session", value: sessionLabel},
			{label: "model", value: status.Model},
			// The two numbers that decide whether to compact or stop live on
			// the shorter row, where a normal terminal still shows them. They
			// are last within it, so the model clips after them, never before.
			{label: "context", value: status.Context},
			{label: "cost", value: status.Cost},
		},
	}
	lines := make([]string, 0, len(groups))
	for index, fields := range groups {
		visible := make([]string, 0, len(fields)+1)
		if index == 0 {
			if lead := permissionLead(status.Approval); lead != "" {
				visible = append(visible, lead)
			}
		}
		for _, field := range fields {
			value := sanitizeTerminalLine(field.value)
			if value != "" {
				visible = append(visible, field.label+" "+value)
			}
		}
		if len(visible) > 0 {
			lines = append(lines, statusIndent+strings.Join(visible, " · "))
		}
	}
	return lines
}

// permissionLead is the tier at a glance, with the key that changes it. One
// chevron per step away from stopping to ask: a tier nobody can see is a tier
// nobody remembers leaving on.
func permissionLead(approval string) string {
	approval = sanitizeTerminalLine(approval)
	if approval == "" {
		return ""
	}
	marker := "⏵"
	switch approval {
	case "auto-approve":
		marker = "⏵⏵"
	case "full-auto":
		marker = "⏵⏵⏵"
	}
	// The key, not a sentence about the key. "(shift+tab to cycle)" costs
	// nine more columns on every row forever, and at 72 columns those nine
	// are the working folder.
	return marker + " " + approval + " (shift+tab)"
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

// agentCount renders the running-subagent count, and nothing when there are
// none.
//
// It is a count and stays one. Item 29 refused resource telemetry on the test
// that nobody could name a decision it would change; a count of running agents
// passes that test because it answers "is this still working, and how wide did
// it go" — a percentage, an elapsed time or a per-agent breakdown would not.
func agentCount(running int) string {
	if running <= 0 {
		return ""
	}
	return strconv.Itoa(running)
}

// queuedCount renders the held-request count. The count is the message: one
// held request already means Enter was pressed against a busy turn.
func queuedCount(queued int) string {
	if queued <= 0 {
		return ""
	}
	return strconv.Itoa(queued)
}
