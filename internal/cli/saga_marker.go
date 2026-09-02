package cli

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// inlineSagaPrompt removes standalone /saga markers from a normal user
// prompt. SAGA is a workflow posture, not a separate command: the remaining
// text is the user's goal and is sent through the ordinary turn boundary.
//
// Whitespace boundaries are deliberate. A URL or path containing "saga" must
// not unexpectedly switch the execution posture, and a marker can appear at
// the beginning, middle, or end of a prompt.
func inlineSagaPrompt(prompt string) (string, bool) {
	var out strings.Builder
	search, copied, found := 0, 0, false
	for search < len(prompt) {
		relative := strings.Index(prompt[search:], "/saga")
		if relative < 0 {
			break
		}
		start := search + relative
		end := start + len("/saga")
		if sagaMarkerBoundary(prompt, start, end) {
			out.WriteString(prompt[copied:start])
			copied = end
			search = end
			found = true
			continue
		}
		search = end
	}
	if !found {
		return prompt, false
	}
	out.WriteString(prompt[copied:])
	return strings.TrimSpace(out.String()), true
}

func sagaMarkerBoundary(prompt string, start, end int) bool {
	if start > 0 {
		before, _ := utf8.DecodeLastRuneInString(prompt[:start])
		if !unicode.IsSpace(before) {
			return false
		}
	}
	if end < len(prompt) {
		after, _ := utf8.DecodeRuneInString(prompt[end:])
		if !unicode.IsSpace(after) {
			return false
		}
	}
	return true
}
