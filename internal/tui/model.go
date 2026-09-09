// Package tui owns Kolkrabbi's interactive terminal screen model. It contains
// no engine or terminal I/O: adapters feed it transcript, activity, status,
// and draft changes as independent regions.
package tui

import (
	"fmt"
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
	// Sandbox is the enforcer confining bash commands, or the word "off".
	// Never empty: an opt-in sandbox's one surviving rule is that its state
	// is always visible.
	Sandbox string
	// Cooling is the one-line notice while the session's connector or model is
	// cooling after a limit; empty, and then absent from the line, otherwise.
	Cooling string
	// Paused is the one-line notice while the session itself is paused on a
	// limit and will resume; empty, and then absent, otherwise.
	Paused    string
	Lifecycle string
	// Context and Cost are the two numbers that decide whether to compact or
	// stop. Empty means not measured yet, which is different from zero.
	Context string
	Cost    string
	// Limits are the plan's windows, drawn as meters on a row of their own
	// in the status area: used in grey, remaining in purple (V38.2).
	Limits []PlanMeter
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
	AgentLogs     map[string][]string
	Suggestions   []CommandSpec
}

// Model contains logical screen state only. One terminal event loop owns a
// Model, so synchronization belongs at that boundary rather than inside every
// field mutation.
type Model struct {
	transcript        []byte
	activity          string
	draft             string
	status            Status
	agentStatuses     []AgentStatus
	agentLogs         map[string][]string
	agentWindowHidden bool
	suggestions       []CommandSpec
	suggestionTop     int
	suggestionWindow  int
	suggestionTotal   int
	selected          int
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
// SetAgentLogs replaces the recent steps of each agent, keyed the way the
// controller keys its statuses; the window shows the last few under each row.
// HideAgentWindow suppresses the compact window over the transcript while
// the full view of the run is open, where it would only repeat it.
func (m *Model) HideAgentWindow(hidden bool) { m.agentWindowHidden = hidden }

func (m *Model) SetAgentLogs(logs map[string][]string) {
	m.agentLogs = make(map[string][]string, len(logs))
	for key, lines := range logs {
		m.agentLogs[key] = append([]string(nil), lines...)
	}
}

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
		AgentLogs:     m.agentLogsCopy(),
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
	// styleUser is the request the user sent: their own words, on their own
	// ground, so a long one can be found in a transcript at a glance.
	styleUser
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
	styleUser:        "\x1b[48;5;236;38;5;147m",
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

// A theme is the two coloured tiers of one look; the colourless tier is the
// same for every theme, because a person who asked out of colour gets none.
// Appearance is all a theme may change: the styles attach to the same
// structural facts and the layout never reads the theme.
type theme struct {
	name string
	c256 palette
	c16  palette
}

var themes = []theme{
	{name: "kolkrabbi", c256: palette256, c16: palette16},
	{name: "nord", c256: palette{
		stylePurple:      "\x1b[38;5;110m",
		stylePurpleMuted: "\x1b[38;5;103m",
		styleHeading:     "\x1b[38;5;110;1m",
		styleMeta:        "\x1b[2m",
		styleAdd:         "\x1b[38;5;108m",
		styleDel:         "\x1b[38;5;131m",
		styleWarn:        "\x1b[38;5;222m",
	}, c16: palette{
		stylePurple:      "\x1b[94m",
		stylePurpleMuted: "\x1b[90m",
		styleHeading:     "\x1b[94;1m",
		styleMeta:        "\x1b[2m",
		styleAdd:         "\x1b[32m",
		styleDel:         "\x1b[31m",
		styleWarn:        "\x1b[33m",
	}},
	// quiet has no hue of its own: bold for headings, dim for meta, and only
	// the diff and warning colours, which carry meaning rather than brand.
	{name: "quiet", c256: palette{
		stylePurple:      "\x1b[1m",
		stylePurpleMuted: "\x1b[2m",
		styleHeading:     "\x1b[1m",
		styleMeta:        "\x1b[2m",
		styleAdd:         "\x1b[32m",
		styleDel:         "\x1b[31m",
		styleWarn:        "\x1b[33m",
	}, c16: palette{
		stylePurple:      "\x1b[1m",
		stylePurpleMuted: "\x1b[2m",
		styleHeading:     "\x1b[1m",
		styleMeta:        "\x1b[2m",
		styleAdd:         "\x1b[32m",
		styleDel:         "\x1b[31m",
		styleWarn:        "\x1b[33m",
	}},
}

// activePalette is process state, not screen state: the terminal's colour
// capability cannot change while kolk is attached to it, and every render
// reads it. The CLI picks the tier once at startup, honouring NO_COLOR, and
// the theme from its setting; /theme changes the theme for the session.
var (
	paletteMu     sync.RWMutex
	activeTier    = "256"
	activeTheme   = themes[0]
	activePalette = palette256
)

// SetPalette selects the escape tier: "256", "16", or "none". It is called
// once by the CLI before the first frame, from the same capability probe the
// legacy line REPL uses for its colour.
func SetPalette(tier string) {
	paletteMu.Lock()
	defer paletteMu.Unlock()
	switch tier {
	case "16", "none":
		activeTier = tier
	default:
		activeTier = "256"
	}
	activePalette = activeTheme.palette(activeTier)
}

// SetTheme selects a look by name; the tier stays whatever the terminal can
// show. An unknown name is refused with the names that exist.
func SetTheme(name string) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		name = themes[0].name
	}
	for _, t := range themes {
		if t.name == name {
			paletteMu.Lock()
			defer paletteMu.Unlock()
			activeTheme = t
			activePalette = t.palette(activeTier)
			return nil
		}
	}
	return fmt.Errorf("no theme %q; the themes are %s", name, strings.Join(Themes(), ", "))
}

// Themes lists the names, in the order they are offered.
func Themes() []string {
	out := make([]string, 0, len(themes))
	for _, t := range themes {
		out = append(out, t.name)
	}
	return out
}

// ActiveTheme is the name in force.
func ActiveTheme() string {
	paletteMu.RLock()
	defer paletteMu.RUnlock()
	return activeTheme.name
}

func (t theme) palette(tier string) palette {
	switch tier {
	case "16":
		return t.c16
	case "none":
		return palette{styleNone: ""}
	}
	return t.c256
}

const resetANSI = "\x1b[0m"

type viewRow struct {
	text  string
	style rowStyle
	// spans, when set, draw the row as a run of differently styled pieces;
	// text is then their concatenation, kept for width and diffing.
	spans []styledSpan
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
	rows, budget, _ := m.layoutWithComposer(width, height, cursor)
	return rows, budget
}

// layoutWithComposer is layout, and says which frame row the composer's top
// rule sits on — what a click has to be measured against.
func (m *Model) layoutWithComposer(width, height, cursor int) ([]viewRow, int, int) {
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
	// The agents' window (plan 37): on a screen wide enough for two columns
	// the rows and their last steps sit top right over the transcript; on a
	// narrow one they keep their old place, a full-width row each.
	window := m.agentWindowLines(width, height)
	agentRows := make([]viewRow, 0, len(m.agentStatuses))
	if window == nil {
		for _, status := range m.agentStatuses {
			agentRows = append(agentRows, viewRow{
				text: clipLine(formatAgentStatusLine(status), width), style: agentStatusStyle(status),
			})
		}
	}
	statusLine := planMetersRow(m.status.Limits, width)
	for _, status := range formatStatus(m.status) {
		statusLine = append(statusLine, viewRow{text: clipLine(status, width), style: stylePurpleMuted})
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
	for height > 0 && len(agentRows)+len(statusLine)+len(composer) > height && len(agentRows) > 0 {
		agentRows = agentRows[1:]
	}
	for height > 0 && len(statusLine)+len(composer) > height && len(statusLine) > 0 {
		// Keep the first row, which carries the permission tier, mode and
		// lifecycle state; the secondary session/model row is less urgent than
		// proving that an active turn is still alive.
		statusLine = statusLine[:len(statusLine)-1]
	}
	for height > 0 && len(activity)+len(composer) > height && len(activity) > 0 {
		// This is only reachable when the terminal has no row beyond the
		// composer for the indicator. Preserve input usability in that
		// impossible-to-share frame; every normal active frame keeps activity.
		activity = nil
	}
	// The indicator sits at the right end of the first status row when it fits:
	// beside the state it describes, below the composer. If it does not fit
	// horizontally, it remains its own row, which the height priority above
	// protects from being crowded out by agent details.
	if len(statusLine) > 0 && len(activity) == 1 &&
		cellWidth(statusLine[0].text)+cellWidth(activity[0].text)+1 <= width {
		statusLine[0].right = activity[0].text
		statusLine[0].rightStyle = stylePurple
		activity = nil
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
		// The request the user sent is styled where it is rendered, as one
		// block including its wrapped and blank rows (V40.2). This used to
		// re-style the marker row here, which could only ever reach the
		// first line of a request.
		rows = append(rows, viewRow{text: row.text, style: row.style})
	}
	// The window takes the right-hand columns of the top transcript rows;
	// each row keeps its own text, clipped so both fit side by side.
	if len(window) > 0 {
		inner := width - agentWindowWidth(width) - 1
		for i := 0; i < len(window) && i < len(rows); i++ {
			rows[i].text = clipLine(rows[i].text, inner)
			rows[i].right = window[i].text
			rows[i].rightStyle = window[i].style
		}
	}
	rows = append(rows, activity...)
	rows = append(rows, agentRows...)
	rows = append(rows, suggestions...)
	composerTop := len(rows)
	rows = append(rows, composer...)
	rows = append(rows, statusLine...)
	return rows, budget, composerTop
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
		if len(row.spans) > 0 {
			for _, span := range row.spans {
				writeStyled(&output, span.text, span.style, styled)
			}
		} else {
			writeStyled(&output, row.text, row.style, styled)
		}
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
			// What confines the commands this session runs, right after how hard
			// it is thinking about them; "off" is a word, never a blank.
			{label: "sandbox", value: status.Sandbox},
			// A remembered limit, only while there is one: the renderer drops an
			// empty value, so nothing cooling means no word about it.
			{label: "cooling", value: status.Cooling},
			// The session's own pause, only while there is one: why it stopped
			// and when it comes back, where the eye already looks for state.
			{label: "paused", value: status.Paused},
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

// The agents' window: the rows and each agent's last steps, top right.

const (
	agentWindowMinScreen = 80
	agentWindowMaxRows   = 24
	agentWindowLogLines  = 3
)

func agentWindowWidth(width int) int {
	return min(56, width*45/100)
}

// agentWindowLines is the window's rows, each already boxed and padded to
// the window's width so the left border lines up; nil when there is nothing
// to show or no room for two columns.
func (m *Model) agentWindowLines(width, height int) []viewRow {
	if len(m.agentStatuses) == 0 || width < agentWindowMinScreen || m.agentWindowHidden {
		return nil
	}
	w := agentWindowWidth(width)
	inner := w - 2
	box := func(text string, style rowStyle) viewRow {
		text = clipLine(text, inner)
		if pad := inner - cellWidth(text); pad > 0 {
			text += strings.Repeat(" ", pad)
		}
		return viewRow{text: "│ " + text, style: style}
	}
	running, total := 0, len(m.agentStatuses)
	for _, status := range m.agentStatuses {
		if status.State == "working" {
			running++
		}
		if status.Total > total {
			total = status.Total
		}
	}

	// Every agent deployed gets a row, whatever else has to go. The window
	// once spent its budget on the first agents' logs and simply stopped,
	// so a run of six showed five while the transcript said six.
	budget := agentWindowBudget(height)
	agents := m.agentStatuses
	overflow := 0
	if needed := 1 + len(agents); needed > budget {
		keep := max(1, budget-2)
		overflow = len(agents) - keep
		agents = agents[:keep]
	}
	logRoom := max(0, budget-1-len(agents))
	if overflow > 0 {
		logRoom = 0
	}

	logs := m.shareLogRoom(agents, logRoom)
	lines := []viewRow{box(fmt.Sprintf("agents %d/%d", running, total), stylePurple)}
	for index, status := range agents {
		lines = append(lines, box(agentWindowRow(status, inner), agentStatusStyle(status)))
		for _, line := range logs[index] {
			lines = append(lines, box("  "+line, styleMeta))
		}
	}
	if overflow > 0 {
		lines = append(lines, box(fmt.Sprintf("+%d more — ← for all of them", overflow), stylePurpleMuted))
	}
	return lines
}

// agentWindowBudget is how many rows the window may take of the screen: enough
// to be worth reading, never so many that the transcript disappears.
func agentWindowBudget(height int) int {
	if height <= 0 {
		return agentWindowMaxRows
	}
	return min(agentWindowMaxRows, max(3, height-5))
}

// agentWindowRow is one agent: what it is doing, how it stands, and on which
// model at what effort — the question a run at mixed efforts raises. The task
// is what gets clipped, so the model and effort survive a narrow window.
func agentWindowRow(status AgentStatus, inner int) string {
	state := compactAgentField(status.State, "working")
	tail := " · " + state
	if model := shortModelName(status.Model); model != "" {
		tail += " · " + model
		if effort := compactAgentField(status.Effort, ""); effort != "" {
			tail += "·" + effort
		}
	}
	head := fmt.Sprintf("%d ", status.Index)
	what := compactAgentField(status.Summary, "task")
	if room := inner - cellWidth(head) - cellWidth(tail); room > 0 {
		what = clipLine(what, room)
	}
	return head + what + tail
}

// shareLogRoom decides how many steps to show under each agent. Every agent
// with something to say gets one line while there is room, and what is left
// over goes to the ones still working: theirs is the line that is about to
// change.
func (m *Model) shareLogRoom(agents []AgentStatus, room int) [][]string {
	available := make([][]string, len(agents))
	for index, status := range agents {
		logs := m.agentLogs[agentKey(status)]
		state := compactAgentField(status.State, "working")
		if step := compactAgentField(status.Step, ""); step != "" && step != state && (len(logs) == 0 || logs[len(logs)-1] != step) {
			logs = append(append([]string(nil), logs...), step)
		}
		available[index] = logs
	}
	shown := make([]int, len(agents))
	// One pass to give everyone a line, then passes that favour the working
	// agents, until the room or the logs run out.
	for round := 0; round < agentWindowLogLines && room > 0; round++ {
		for index, logs := range available {
			if room == 0 {
				break
			}
			if round > 0 && agents[index].State != "working" {
				continue
			}
			if shown[index] >= len(logs) || shown[index] >= agentWindowLogLines {
				continue
			}
			shown[index]++
			room--
		}
	}
	out := make([][]string, len(agents))
	for index, logs := range available {
		if shown[index] > 0 {
			out[index] = logs[len(logs)-shown[index]:]
		}
	}
	return out
}

// shortModelName is the model as a person says it: the part that tells one
// model from another, without the vendor and the route it came by.
func shortModelName(model string) string {
	model = compactAgentField(model, "")
	if model == "" {
		return ""
	}
	if cut := strings.LastIndex(model, "/"); cut >= 0 {
		model = model[cut+1:]
	}
	// The last part names the model where it is a word — haiku, fable, luna —
	// and where it is a version or a size the vendor prefix goes instead, so
	// claude-fable-5-1 stays fable-5-1 rather than becoming "1".
	if cut := strings.LastIndex(model, "-"); cut >= 0 && isLetters(model[cut+1:]) {
		return model[cut+1:]
	}
	if cut := strings.Index(model, "-"); cut > 0 && cut+1 < len(model) {
		model = model[cut+1:]
	}
	return model
}

func isLetters(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

// agentKey names an agent the way the controller does, so the logs it keeps
// per key are found here.
func agentKey(status AgentStatus) string {
	if status.ID != "" {
		return status.ID
	}
	return fmt.Sprintf("agent-%d", status.Index)
}

func (m *Model) agentLogsCopy() map[string][]string {
	if len(m.agentLogs) == 0 {
		return nil
	}
	out := make(map[string][]string, len(m.agentLogs))
	for key, lines := range m.agentLogs {
		out[key] = append([]string(nil), lines...)
	}
	return out
}

// PlanMeter is one plan window for the status meters: a short label and
// the share used, 0..1.
type PlanMeter struct {
	Label string
	Used  float64
}

// styledSpan is one piece of a row drawn in its own style.
type styledSpan struct {
	text  string
	style rowStyle
}

// planMeterCells is the width of one meter's bar.
const planMeterCells = 12

// planMetersRow draws every plan window as a meter: label, the used share
// in grey, what remains in purple, the percent used. Nil without windows.
func planMetersRow(limits []PlanMeter, width int) []viewRow {
	if len(limits) == 0 {
		return nil
	}
	spans := []styledSpan{{text: statusIndent, style: stylePurpleMuted}}
	for i, limit := range limits {
		if i > 0 {
			spans = append(spans, styledSpan{text: " · ", style: stylePurpleMuted})
		}
		used := min(max(limit.Used, 0), 1)
		usedCells := int(used*planMeterCells + 0.5)
		label := sanitizeTerminalLine(limit.Label)
		// Heavy for what is spent, light for what is left: the bar reads
		// under NO_COLOR too, where grey and purple are the same ink.
		spans = append(spans,
			styledSpan{text: label + " ", style: stylePurpleMuted},
			styledSpan{text: strings.Repeat("━", usedCells), style: styleMeta},
			styledSpan{text: strings.Repeat("─", planMeterCells-usedCells), style: stylePurple},
			styledSpan{text: fmt.Sprintf(" %d%%", int(used*100+0.5)), style: stylePurpleMuted},
		)
	}
	text := ""
	for _, span := range spans {
		text += span.text
	}
	if cellWidth(text) > width {
		// Too narrow for the meters: the percents alone, one plain row.
		parts := make([]string, 0, len(limits))
		for _, limit := range limits {
			parts = append(parts, fmt.Sprintf("%s %d%%", sanitizeTerminalLine(limit.Label), int(min(max(limit.Used, 0), 1)*100+0.5)))
		}
		return []viewRow{{text: clipLine(statusIndent+strings.Join(parts, " · "), width), style: stylePurpleMuted}}
	}
	return []viewRow{{text: text, style: stylePurpleMuted, spans: spans}}
}

// ComposerHit maps a click inside the frame to a rune offset in the draft.
// col and row are zero-based cells from the frame's top-left corner; ok is
// false for a click anywhere but the composer's own text rows.
//
// The hit is measured against the draft as it wraps without the caret glyph,
// so a click lands on the character under the pointer; a click past the end
// of a line goes to the end of that line, which is where a caret is wanted
// anyway.
func (m *Model) ComposerHit(width, height, cursor, col, row int) (int, bool) {
	_, _, composerTop := m.layoutWithComposer(width, height, cursor)
	// The composer opens with its top rule and closes with another; the text
	// is what lies between.
	first := composerTop + 1
	lines := m.composerRunes(width)
	if row < first || row >= first+len(lines) {
		return 0, false
	}
	line := lines[row-first]
	// Every content row is written behind a two-cell prefix: the prompt
	// marker on the first, blanks on the rest.
	target := col - 2
	offset := line.start
	if target <= 0 {
		return offset, true
	}
	used := 0
	for _, r := range line.runes {
		cells := runeCellWidth(r)
		if used+cells > target {
			break
		}
		used += cells
		offset++
	}
	return offset, true
}

// composerLine is one wrapped row of the draft with the rune offset it opens
// at.
type composerLine struct {
	start int
	runes []rune
}

// composerRunes wraps the draft the way composerLines draws it, without the
// caret, keeping each row's offset into the draft.
func (m *Model) composerRunes(width int) []composerLine {
	contentWidth := max(1, width-2)
	var lines []composerLine
	offset := 0
	for index, paragraph := range strings.Split(sanitizeTerminalText(m.draft), "\n") {
		if index > 0 {
			offset++ // the newline itself
		}
		for _, wrapped := range wrapLine(paragraph, contentWidth) {
			runes := []rune(wrapped)
			lines = append(lines, composerLine{start: offset, runes: runes})
			offset += len(runes)
		}
	}
	return lines
}
