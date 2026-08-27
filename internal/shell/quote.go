package shell

import "strings"

// Quote wraps text as one POSIX shell word.
//
// It lives here because three callers grew their own copy: the saga quoting a
// model-written chapter title, the shadow store quoting a snapshot message, and
// `/pr` quoting a drafted title into the `gh pr create` it hands over. Each is
// the same boundary — the place where a quote in someone else's text stops
// being punctuation and starts being syntax — and three copies of that boundary
// is three chances for one of them to be subtly different.
//
// It was deleted once for having no callers, which was right at the time. Three
// is the number that earns it back.
func Quote(text string) string {
	return "'" + strings.ReplaceAll(text, "'", `'\''`) + "'"
}
