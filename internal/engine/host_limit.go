package engine

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// hostRefusal reports an error that came from the user's own Ollama — a local
// model, or a cloud model proxied through it. Neither belongs to the two
// policies the retry path applies to gateway errors:
//
//   - routing.on_subscription_limit answers "your plan ran out, may I bill
//     you?" — but Ollama Cloud's limit resets (session limits every 5 h, weekly
//     every 7 d), and its wording ("usage limit") would otherwise match A33.7's
//     allowance phrases and trip the metered fallback, even under `switch`.
//   - routing.on_free_exhausted rotates free gateway models through a 1-2-4 s
//     backoff. A local server does not rate-limit, and a limit that resets in
//     hours is not cleared by four seconds.
//
// So a host refusal is returned once, unretried, with a sentence that says
// what it is. This is the third cost class the TUI already names (CostLocal),
// applied where the money decisions are made.
func hostRefusal(err error) (*provider.HTTPError, bool) {
	var httpErr *provider.HTTPError
	if errors.As(err, &httpErr) && httpErr.Origin == provider.HostOrigin {
		return httpErr, true
	}
	return nil, false
}

// explainHostRefusal wraps a host refusal so the transcript says what it is
// rather than what a gateway error of the same status would have meant.
func explainHostRefusal(model string, httpErr *provider.HTTPError, err error) error {
	if httpErr.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("%s: Ollama Cloud's usage limit is reached; it resets on a schedule (session limits every 5 hours, weekly ones every 7 days), and nothing here is billed instead. A local model has no limit — `/model` lists what is pulled: %w", model, err)
	}
	return err
}
