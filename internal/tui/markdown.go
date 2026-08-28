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
		if heading, ok := trimHeading(line); ok {
			for _, wrapped := range wrapLine(heading, width) {
				rows = append(rows, styledRow{text: wrapped, style: styleHeading})
			}
			rows = append(rows, styledRow{})
			index++
			boundaries = append(boundaries, blockBoundary{source: index, rendered: len(rows)})
			continue
		}
		rows = append(rows, wrapMarkdownLine(markdownLine(line), width)...)
		index++
		boundaries = append(boundaries, blockBoundary{source: index, rendered: len(rows)})
	}
	return rows, boundaries
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
	for _, row := range body {
		row = clipRow(row, contentWidth)
		if info.isDiff {
			// The style is read from the signed row and only then is the sign
			// given its own column; reading it after diffPrefix would see the
			// payload only and colour nothing.
			style := diffStyle(row)
			prefixed := diffPrefix(row)
			// Sign-coloured diff rows are the whole reason the sign sits
			// alone in its column: the eye can then run down the edge.
			rows = append(rows, styledRow{text: clipLine(prefix+prefixed, width), style: style})
			continue
		}
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
func wrapMarkdownLine(line string, width int) []styledRow {
	marker, body, indent := splitHangingIndent(line)
	if marker == "" {
		wrapped := wrapWords(body, width)
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
// column zero, where the prefix shape would be lost.
func splitHangingIndent(line string) (marker, body, indent string) {
	switch {
	case strings.HasPrefix(line, quoteToken):
		return quoteToken, strings.TrimPrefix(line, quoteToken), ""
	case strings.HasPrefix(line, "  "+listToken):
		return listToken, strings.TrimPrefix(line, "  "+listToken), "  "
	default:
		return "", line, ""
	}
}

// diffStyle classifies one raw diff row: added, removed, or meta. Context
// rows and hunk headers dim so the +/- lines are the only colour in the block.
func diffStyle(row string) rowStyle {
	switch {
	case strings.HasPrefix(row, "+"):
		return styleAdd
	case strings.HasPrefix(row, "-"):
		return styleDel
	case strings.HasPrefix(row, "@@"), strings.HasPrefix(row, "diff --git"),
		strings.HasPrefix(row, "index "), strings.HasPrefix(row, "---"), strings.HasPrefix(row, "+++"):
		return styleMeta
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
