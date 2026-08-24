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
func renderMarkdown(text string, width int) []string {
	sanitized := sanitizeTerminalText(text)
	logical := strings.Split(sanitized, "\n")
	if len(logical) > 0 && logical[len(logical)-1] == "" {
		logical = logical[:len(logical)-1]
	}

	var lines []string
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
			lines = append(lines, renderCodeBlock(info, body, closed, width)...)
			if index < len(logical) {
				// Step past whichever row ended the scan: a closing fence,
				// a blank prose terminator, or nothing at end of input.
				index++
			}
			continue
		}
		if heading, ok := trimHeading(line); ok {
			lines = append(lines, wrapLine(heading, width)...)
			lines = append(lines, "")
			index++
			continue
		}
		lines = append(lines, wrapLine(markdownLine(line), width)...)
		index++
	}
	return lines
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

func renderCodeBlock(info fence, body []string, closed bool, width int) []string {
	title := "code"
	if info.isDiff {
		title = diffMarker
	} else if info.language != "" {
		title = info.language
	}

	contentWidth := max(1, width-3)
	lines := make([]string, 0, len(body)+2)
	rows := make([]string, 0, len(body))
	for _, row := range body {
		rows = append(rows, clipRow(row, contentWidth))
	}
	lines = append(lines, clipLine("╭─ "+title, width))
	prefix := "│ "
	if info.isDiff {
		for i, row := range rows {
			rows[i] = diffPrefix(row)
		}
	}
	for _, row := range rows {
		for _, wrapped := range wrapLine(row, contentWidth) {
			lines = append(lines, clipLine(prefix+wrapped, width))
		}
	}
	if closed {
		lines = append(lines, clipLine("╰─", width))
	}
	return lines
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
