package tui

import (
	"strings"
)

// This file turns streamed Markdown-ish transcript text into the same visual
// tokens the composer uses (╭─/│/╰─) with no third-party dependency. It is a
// deterministic presentation pass only: it never mutates stored transcript
// bytes, resolves engine policy, or interprets code/diff contents.

const (
	codeFence  = "```"
	diffMarker = "diff"
	quoteToken = "  │ "
	listToken  = "· "
)

// renderMarkdown renders one transcript chunk as terminal rows. Structural
// parsing happens after sanitizeTerminalText, so escape sequences cannot
// masquerade as fences, headings, or diff markers.
// blockBoundary is a point where the renderer holds no state: no fence is
// open, so rendering the source up to here produces exactly the prefix that
// rendering the whole would. That is what makes it safe to cut the transcript
// here and hand the lines above to the terminal's scrollback.
type blockBoundary struct {
	source   int // logical source lines before this point
	rendered int // rendered lines before this point
}

// styledRow is one rendered terminal row and the fixed style it carries.
// Styles attach to structural facts established here, at the only place that
// knows a row was a diff line or a heading; everything downstream joins them
// into strings.
type styledRow struct {
	text  string
	style rowStyle
}

// renderMarkdownBlocks renders text and reports every point at which the
// rendering could be cut without changing what either half looks like.
func renderMarkdownBlocks(text string, width int) ([]string, []blockBoundary) {
	rows, boundaries := renderMarkdownStyledBlocks(text, width)
	return rowTexts(rows), boundaries
}

func rowTexts(rows []styledRow) []string {
	out := make([]string, len(rows))
	for index, row := range rows {
		out[index] = row.text
	}
	return out
}

func renderMarkdownStyled(text string, width int) []styledRow {
	rows, _ := renderMarkdownStyledBlocks(text, width)
	return rows
}

func renderMarkdownStyledBlocks(text string, width int) ([]styledRow, []blockBoundary) {
	sanitized := sanitizeTerminalText(text)
	logical := strings.Split(sanitized, "\n")
	if len(logical) > 0 && logical[len(logical)-1] == "" {
		logical = logical[:len(logical)-1]
	}

	var rows []styledRow
	// The top of this loop is a boundary by construction: every block the body
	// handles is consumed whole before control returns here.
	boundaries := []blockBoundary{{}}
	// work is true inside a work record: from its "• " heading until a blank
	// line or a line of prose, so only its own supporting rows are muted.
	work := false
	for index := 0; index < len(logical); {
		line := strings.TrimRight(logical[index], " \t")
		if info, ok := fenceInfo(line); ok {
			var body []string
			closed := false
			for index++; index < len(logical); index++ {
				row := logical[index]
				if strings.TrimRight(row, " \t") == codeFence {
					closed = true
					break
				}
				if row == "" && !info.isDiff {
					// A blank line ends an unterminated prose fence so a
					// stray opener cannot swallow the rest of the answer.
					break
				}
				body = append(body, row)
			}
			rows = append(rows, renderCodeBlock(info, body, closed, width)...)
			if index < len(logical) {
				// Step past whichever row ended the scan: a closing fence,
				// a blank prose terminator, or nothing at end of input.
				index++
			}
			// An unclosed fence is still being written, so its rendering can
			// still change: it is not a boundary.
			if closed {
				boundaries = append(boundaries, blockBoundary{source: index, rendered: len(rows)})
			}
			continue
		}
		// The request the user sent is one block: the marker row and the
		// rows indented under it, styled together and before wrapping, so a
		// long line stays theirs all the way across. The block ends at the
		// first row that is neither — a following request opens its own.
		// An indented line of kolk's own output written straight after a
		// request, with no blank row between, would be taken for part of it;
		// nothing prints that today and the cost of being wrong is a shaded
		// line.
		// Like the rows under it, the marker row is tested raw: a request
		// that opens with a blank line is "❯ " alone, which trimming turns
		// into a line this no longer recognises.
		if strings.HasPrefix(logical[index], promptMarker+" ") {
			for start := index; index < len(logical); index++ {
				// The prefix is tested on the raw row, not the trimmed one:
				// a blank line inside the request is written as its own
				// indent, and trimming first made it look like the end of
				// the block — which cut the owner's own message in two.
				if index > start && !strings.HasPrefix(logical[index], "  ") {
					break
				}
				for _, wrapped := range wrapLine(strings.TrimRight(logical[index], " \t"), width) {
					rows = append(rows, styledRow{text: wrapped, style: styleUser})
				}
			}
			boundaries = append(boundaries, blockBoundary{source: index, rendered: len(rows)})
			continue
		}
		if heading, ok := trimHeading(line); ok {
			for _, wrapped := range wrapLine(heading, width) {
				rows = append(rows, styledRow{text: wrapped, style: styleHeading})
			}
			rows = append(rows, styledRow{})
			index++
			boundaries = append(boundaries, blockBoundary{source: index, rendered: len(rows)})
			continue
		}
		if strings.HasPrefix(line, "• ") {
			// Only kolk's own records end with their owner label; an
			// assistant bullet starts no record, so nothing under it mutes.
			work = hasWorkOwner(strings.TrimPrefix(line, "• "))
			// A wrapped heading hangs under its text, not under the bullet,
			// and its owner label moves whole.
			body := keepWorkOwner(keepWorkCounts(strings.TrimPrefix(line, "• ")))
			for index, wrapped := range wrapWords(body, max(1, width-2)) {
				lead := "  "
				if index == 0 {
					lead = "• "
				}
				rows = append(rows, styledRow{text: lead + strings.ReplaceAll(wrapped, noBreak, " "), style: styleWork})
			}
		} else {
			// A record continues only through its own row shapes; anything
			// else, such as prose that follows it, ends the record.
			work = work && continuesWork(logical[index])
			style := transcriptRowStyle(line, work)
			for _, row := range wrapMarkdownLine(markdownLine(line), width) {
				if row.style == styleNone {
					row.style = style
				}
				rows = append(rows, row)
			}
		}
		index++
		// A work record is one block: cut inside it, the rows after the cut
		// would render without their heading, and so without its colour. At
		// the end of the transcript a record is still open, like an unclosed
		// fence: grouped exploration appends rows under its heading later.
		if work && (index == len(logical) || continuesWork(logical[index])) {
			continue
		}
		boundaries = append(boundaries, blockBoundary{source: index, rendered: len(rows)})
	}
	return rows, boundaries
}

// continuesWork says whether a raw transcript row is one of a work record's
// own shapes: a result (└) or a 4-space purpose or output row. Both are tested
// raw: a blank line of output is the branch or the indent alone, which
// trimming would make look like the end of a record.
func continuesWork(raw string) bool {
	return strings.HasPrefix(raw, "  └ ") || strings.HasPrefix(raw, "    ")
}

// transcriptRowStyle gives kolk's own supporting rows their colour: inside a
// work record the results, purposes and output are muted and a failure is
// red; outside one, engine activity ("◆ …") is purple and the per-turn usage
// line is muted. Everything else keeps the plain style.
func transcriptRowStyle(line string, work bool) rowStyle {
	trimmed := strings.TrimSpace(line)
	switch {
	case work && strings.HasPrefix(line, "  └ × "):
		return styleDel
	case work:
		return stylePurpleMuted
	case strings.HasPrefix(line, "◆ "):
		return stylePurple
	case strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "ms]") && strings.Contains(trimmed, " · "):
		return stylePurpleMuted
	}
	return styleNone
}

type fence struct {
	language string
	isDiff   bool
}

// fenceInfo recognizes an opening fence and its info string. A closing fence
// must sit on its own line, so prose containing backticks is never swallowed.
func fenceInfo(line string) (fence, bool) {
	if !strings.HasPrefix(line, codeFence) {
		return fence{}, false
	}
	info := strings.TrimSpace(line[len(codeFence):])
	if strings.ContainsAny(info, "` ") && !strings.Contains(info, diffMarker) {
		return fence{}, false
	}
	if info == diffMarker || strings.HasPrefix(strings.ToLower(info), diffMarker) {
		return fence{language: info, isDiff: true}, true
	}
	return fence{language: info}, true
}

func renderCodeBlock(info fence, body []string, closed bool, width int) []styledRow {
	title := "code"
	if info.isDiff {
		title = diffMarker
	} else if info.language != "" {
		title = info.language
	}

	contentWidth := max(1, width-3)
	rows := make([]styledRow, 0, len(body)+2)
	rows = append(rows, styledRow{text: clipLine("╭─ "+title, width), style: styleMeta})
	prefix := "│ "
	var numbers diffLineNumbers
	for _, row := range body {
		if info.isDiff {
			// The style is read from the signed row and only then is the sign
			// given its own column; reading it after diffPrefix would see the
			// payload only and colour nothing.
			style := diffStyle(row)
			prefixed := numbers.prefix(row, width)
			// Sign-coloured diff rows are the whole reason the sign sits
			// alone in its column: the eye can then run down the edge.
			rows = append(rows, styledRow{text: clipLine(prefix+prefixed, width), style: style})
			continue
		}
		row = clipRow(row, contentWidth)
		for _, wrapped := range wrapLine(row, contentWidth) {
			rows = append(rows, styledRow{text: clipLine(prefix+wrapped, width)})
		}
	}
	if closed {
		rows = append(rows, styledRow{text: clipLine("╰─", width), style: styleMeta})
	}
	return rows
}

// wrapMarkdownLine wraps one mapped prose row, preferring word boundaries and
// keeping the shape the first line established: a list item's continuation
// lines stay under its text, a quote's stay inside its bar. Breaking mid-word
// on every long paragraph is what makes a transcript read as broken text.
// minHangText is the least room a hanging indent must leave its text.
// Narrower than that, the hang could only clip the text to "…", so the line
// wraps like prose instead, as diff line numbers give way on narrow rows.
const minHangText = 8

func wrapMarkdownLine(line string, width int) []styledRow {
	marker, body, indent := splitHangingIndent(line)
	if marker == "" && indent == "" || width-cellWidth(indent)-cellWidth(marker) < minHangText {
		wrapped := wrapWords(line, width)
		rows := make([]styledRow, len(wrapped))
		for index, text := range wrapped {
			rows[index] = styledRow{text: text}
		}
		return rows
	}
	// The marker is part of every row, not just the first: the wrap width is
	// what is left after both the hanging indent and the marker. Forgetting the
	// marker wrapped the body too wide and clipLine then dropped whole words
	// off the right edge — rows that looked complete but said less.
	contentWidth := max(1, width-cellWidth(indent)-cellWidth(marker))
	wrapped := wrapWords(body, contentWidth)
	rows := make([]styledRow, 0, len(wrapped))
	rows = append(rows, styledRow{text: clipLine(indent+marker+wrapped[0], width)})
	for _, rest := range wrapped[1:] {
		rows = append(rows, styledRow{text: clipLine(indent+strings.Repeat(" ", cellWidth(marker))+rest, width)})
	}
	return rows
}

// splitHangingIndent takes a mapped markdown line back apart into its bullet
// shape so wrapping can put continuation rows under the text instead of at
// column zero, where the prefix shape would be lost. Numbered items, kolk's
// ◆ notices and the work log's result, purpose and output rows hang the same
// way.
func splitHangingIndent(line string) (marker, body, indent string) {
	switch {
	case strings.HasPrefix(line, quoteToken):
		return quoteToken, strings.TrimPrefix(line, quoteToken), ""
	case strings.HasPrefix(line, "  "+listToken):
		return listToken, strings.TrimPrefix(line, "  "+listToken), "  "
	case numberedMarker(line) != "":
		marker := numberedMarker(line)
		return marker, strings.TrimPrefix(line, "  "+marker), "  "
	case strings.HasPrefix(line, "  ◆ "):
		return "◆ ", strings.TrimPrefix(line, "  ◆ "), "  "
	case strings.HasPrefix(line, "◆ "):
		return "◆ ", strings.TrimPrefix(line, "◆ "), ""
	case strings.HasPrefix(line, "  └ "):
		return "└ ", strings.TrimPrefix(line, "  └ "), "  "
	case strings.HasPrefix(line, "    ↳ "):
		return "↳ ", strings.TrimPrefix(line, "    ↳ "), "    "
	case strings.HasPrefix(line, "    "):
		return "", strings.TrimPrefix(line, "    "), "    "
	default:
		return "", line, ""
	}
}

// numberedMarker is the "N. " of an ordered item as markdownLine maps it, and
// as kolk writes its plan, two spaces in; "" for any other line.
func numberedMarker(line string) string {
	rest, ok := strings.CutPrefix(line, "  ")
	if !ok {
		return ""
	}
	digits := 0
	for digits < len(rest) && rest[digits] >= '0' && rest[digits] <= '9' {
		digits++
	}
	if digits == 0 || !strings.HasPrefix(rest[digits:], ". ") {
		return ""
	}
	return rest[:digits+2]
}

// noBreak binds words that wrapping must keep on one row; it is put back as
// an ordinary space once the row is cut.
const noBreak = "\u00a0"

// workOwner is a work heading's closing " · <owner>" label — kolk, or
// agent N — and where it starts; ok is false for any other heading.
func workOwner(heading string) (cut int, owner string, ok bool) {
	cut = strings.LastIndex(heading, " · ")
	if cut < 0 {
		return 0, "", false
	}
	owner = heading[cut+len(" · "):]
	number, agent := strings.CutPrefix(owner, "agent ")
	if owner != "kolk" && (!agent || number == "" || strings.Trim(number, "0123456789") != "") {
		return 0, "", false
	}
	return cut, owner, true
}

// hasWorkOwner reports a heading kolk wrote for one of its work records.
func hasWorkOwner(heading string) bool {
	_, _, ok := workOwner(heading)
	return ok
}

// keepWorkCounts binds a heading's "(+12 -3)" into one word, so a wrap never
// leaves "(+12" on one row and "-3)" on the next, where writeWorkHeading sees
// neither half as counts and colours neither. The first "(+" is the one it
// colours, so that is the one bound.
func keepWorkCounts(heading string) string {
	start := strings.Index(heading, "(+")
	if start < 0 {
		return heading
	}
	end := strings.IndexByte(heading[start:], ')')
	if end < 0 {
		return heading
	}
	end += start
	added, removed, ok := strings.Cut(heading[start+2:end], " -")
	if !ok || added == "" || removed == "" || strings.Trim(added+removed, "0123456789") != "" {
		return heading
	}
	return heading[:start] + "(+" + added + noBreak + "-" + removed + heading[end:]
}

// keepWorkOwner binds a work heading's closing " · <owner>" into one word,
// so wrapping moves the label whole with its separator.
func keepWorkOwner(heading string) string {
	cut, owner, ok := workOwner(heading)
	if !ok {
		return heading
	}
	return heading[:cut] + " ·" + noBreak + strings.ReplaceAll(owner, " ", noBreak)
}

// diffStyle classifies one raw diff row: added, removed, or meta. Context
// rows and hunk headers dim so the +/- lines are the only colour in the block.
func diffStyle(row string) rowStyle {
	switch {
	case strings.HasPrefix(row, "@@"), strings.HasPrefix(row, "diff --git"),
		strings.HasPrefix(row, "index "):
		return styleMeta
	case strings.HasPrefix(row, "+"):
		return styleDiffAdd
	case strings.HasPrefix(row, "-"):
		return styleDiffDel
	default:
		return styleNone
	}
}

// diffPrefix separates the sign from the payload so +/-/space markers stay
// scannable at any width without coloring.
func diffPrefix(row string) string {
	switch {
	case strings.HasPrefix(row, "+"), strings.HasPrefix(row, "-"):
		return row[:1] + " " + row[1:]
	default:
		return "  " + row
	}
}

// markdownLine maps one non-code Markdown line to plain terminal text using
// fixed tokens instead of ANSI styling.
func markdownLine(line string) string {
	switch {
	case line == "":
		return ""
	case strings.HasPrefix(line, ">"):
		return quoteToken + strings.TrimLeft(line[1:], " ")
	}
	if heading, ok := trimHeading(line); ok {
		return heading
	}
	if marker, rest, ok := listItem(line); ok {
		return "  " + marker + rest
	}
	return line
}

// trimHeading drops ATX hashes; the transcript already reads as an outline,
// and underline-style headings are left untouched rather than guessed.
func trimHeading(line string) (string, bool) {
	if !strings.HasPrefix(line, "#") {
		return "", false
	}
	body := strings.TrimLeft(line, "#")
	if body == line || body == "" || body[0] != ' ' {
		return "", false
	}
	return strings.TrimLeft(body, " "), true
}

// listItem keeps ordered numbers verbatim and normalizes every bullet shape
// to one token.
func listItem(line string) (marker, rest string, ok bool) {
	if rest, found := strings.CutPrefix(line, "- "); found {
		return listToken, rest, true
	}
	if rest, found := strings.CutPrefix(line, "* "); found {
		return listToken, rest, true
	}
	digits := 0
	for digits < len(line) && line[digits] >= '0' && line[digits] <= '9' {
		digits++
	}
	if digits > 0 && digits < len(line) && line[digits] == '.' &&
		digits+1 < len(line) && line[digits+1] == ' ' {
		return line[:digits+1] + " ", line[digits+2:], true
	}
	return "", "", false
}

// clipRow bounds one raw code row by cells before prefixing, so wide runes can
// never push a prefixed row past the terminal edge after wrapping.
func clipRow(row string, width int) string {
	if cellWidth(row) <= width {
		return row
	}
	var used int
	for index, r := range row {
		cells := runeCellWidth(r)
		if used+cells > width {
			return row[:index]
		}
		used += cells
	}
	return row
}
