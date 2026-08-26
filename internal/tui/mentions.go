package tui

import "strings"

// mentionMark introduces a file reference in the composer.
const mentionMark = "@"

// SuggestFiles completes an `@` mention against the project's files.
//
// Only the mention currently being typed is completed, and only that part of
// the line is rewritten: the rest of what someone typed is not the
// completion's to touch.
func SuggestFiles(files []string, draft string, limit int) []CommandSpec {
	if limit <= 0 {
		limit = 8
	}
	prefix, filter, ok := splitMention(draft)
	if !ok {
		return nil
	}

	lowered := strings.ToLower(filter)
	suggestions := make([]CommandSpec, 0, min(limit, len(files)))
	for _, file := range files {
		if lowered != "" && !strings.Contains(strings.ToLower(file), lowered) {
			continue
		}
		suggestions = append(suggestions, CommandSpec{
			Name:     file,
			Usage:    mentionMark + file,
			Complete: prefix + mentionMark + file,
		})
		if len(suggestions) == limit {
			break
		}
	}
	return suggestions
}

// splitMention finds the mention being typed at the end of the draft, and
// returns everything before it.
//
// The mention has to be the last token and has to start one: `a@b.com` is an
// email address, not a reference to a file called b.com.
func splitMention(draft string) (prefix, filter string, ok bool) {
	at := strings.LastIndex(draft, mentionMark)
	if at < 0 {
		return "", "", false
	}
	if at > 0 && !isMentionBoundary(draft[at-1]) {
		return "", "", false
	}
	filter = draft[at+len(mentionMark):]
	if strings.ContainsAny(filter, " \t") {
		// The mention is finished and someone has moved on; completing it now
		// would rewrite text they are no longer editing.
		return "", "", false
	}
	return draft[:at], filter, true
}

func isMentionBoundary(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n'
}
