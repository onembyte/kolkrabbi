package engine

import (
	"errors"
	"net/http"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// overflowPhrases are how providers say "your request does not fit". There is
// no typed signal for this in an OpenAI-compatible API, so it is matched on
// text.
//
// The asymmetry justifies the heuristic here, where it would not elsewhere: a
// false positive costs one compaction and one retry, both visible and both
// recoverable, while a false negative just leaves today's behaviour of failing
// the turn. Nothing is disabled and nothing is lost either way.
var overflowPhrases = []string{
	"context length",
	"context_length_exceeded",
	"context window",
	"too long",
	"too large",
	"reduce the length",
	"maximum context",
}

// IsContextOverflow reports whether a provider refused a request because it did
// not fit the model's window.
func IsContextOverflow(err error) bool {
	var httpErr *provider.HTTPError
	if !errors.As(err, &httpErr) {
		return false
	}
	if httpErr.StatusCode != http.StatusBadRequest && httpErr.StatusCode != http.StatusRequestEntityTooLarge {
		return false
	}
	haystack := strings.ToLower(httpErr.Message + " " + httpErr.ResponseBody)
	for _, phrase := range overflowPhrases {
		if strings.Contains(haystack, phrase) {
			return true
		}
	}
	return false
}
