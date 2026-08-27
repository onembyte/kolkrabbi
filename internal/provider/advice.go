package provider

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// Advice is what to tell a person when a request to a provider fails: one line
// saying what happened, and one line they can act on.
//
// Both are required. A failure with no next action is where a user stops using
// a tool, because the alternative to being told what to do is guessing — and
// the guesses available here (wrong key? wrong model? no credit? provider
// down?) all look the same from the outside.
type Advice struct {
	Summary    string
	NextAction string
	// RetryAfter is the provider's own answer to "how long", when it gave one.
	// Zero means it did not.
	RetryAfter time.Duration
}

// toolPhrases are how providers say "this model cannot call tools". Like the
// overflow phrases, there is no typed signal for it in an OpenAI-compatible
// API, and this one matters more than most: a model without tool support
// answers normally right up until the moment it has to use one, so the failure
// looks like kolk breaking rather than a model being the wrong model.
var toolPhrases = []string{
	"tool use",
	"tool_use",
	"tool calling",
	"tool_calls",
	"function calling",
	"does not support tools",
}

// Advise maps a provider failure onto something worth reading. The second
// return is false when Advise has nothing to add — an unrelated error, a
// cancelled context, or nil — so callers print their own message rather than a
// vague one of ours.
func Advise(err error) (Advice, bool) {
	if err == nil {
		return Advice{}, false
	}

	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		return adviseTransport(err)
	}

	switch httpErr.StatusCode {
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge:
		return adviseBadRequest(err, httpErr)
	case http.StatusUnauthorized:
		return Advice{
			Summary:    "OpenRouter rejected the API key",
			NextAction: "Set a working key with `kolk key <API_KEY>`, or check OPENROUTER_API_KEY if you export one.",
		}, true
	case http.StatusPaymentRequired:
		return Advice{
			Summary:    "this model costs money and the account is out of credit",
			NextAction: "Add credit at openrouter.ai, or pick a free model — `kolk models` lists them, and ids ending in `:free` cost nothing.",
		}, true
	case http.StatusForbidden:
		return Advice{
			Summary:    "the provider refused this request outright",
			NextAction: "This is usually a region or content restriction on the model, not a kolk setting. `kolk models` lists alternatives.",
		}, true
	case http.StatusNotFound:
		return Advice{
			Summary:    "no model is served under that id",
			NextAction: "Run `kolk models` for current ids — they change, and an id that worked last month can stop being served.",
		}, true
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return Advice{
			Summary:    "the provider timed out before answering",
			NextAction: "Try again. If it keeps happening on one model, another one is likely faster to first token.",
		}, true
	case http.StatusTooManyRequests:
		return adviseRateLimited(httpErr), true
	}

	if httpErr.StatusCode >= 500 {
		return Advice{
			Summary:    fmt.Sprintf("OpenRouter or the model behind it is having trouble (HTTP %d)", httpErr.StatusCode),
			NextAction: "Nothing is wrong on this side. Try again in a moment, or `kolk models` to route around one bad provider.",
		}, true
	}
	if httpErr.StatusCode >= 400 {
		return Advice{
			Summary:    fmt.Sprintf("the request was refused (HTTP %d)", httpErr.StatusCode),
			NextAction: "The provider's own message is above. `kolk models` lists what is currently served.",
		}, true
	}
	return Advice{}, false
}

func adviseBadRequest(err error, httpErr *HTTPError) (Advice, bool) {
	// Overflow first: it arrives as a 400, and "the request was refused" is the
	// wrong thing to say about a conversation that simply grew too long.
	if IsContextOverflow(err) {
		return Advice{
			Summary:    "the conversation is longer than this model's context window",
			NextAction: "kolk compacts and retries once on its own. If it persists, `/compact` or start a fresh session — or pick a model with a larger window from `kolk models`.",
		}, true
	}
	haystack := strings.ToLower(httpErr.Message + " " + httpErr.ResponseBody)
	for _, phrase := range toolPhrases {
		if strings.Contains(haystack, phrase) {
			return Advice{
				Summary:    "this model cannot call tools, and code mode needs them",
				NextAction: "Pick a tool-capable model: `kolk models` marks which ones qualify. Chat mode works with any model.",
			}, true
		}
	}
	return Advice{
		Summary:    "the provider rejected the request as malformed",
		NextAction: "The provider's own message is above; it usually names the parameter it disliked.",
	}, true
}

func adviseRateLimited(httpErr *HTTPError) Advice {
	next := "kolk rotates to another free model on its own when one is available. Waiting a minute, or using a paid model, both work."
	if httpErr.RetryAfter > 0 {
		next = fmt.Sprintf("The provider asked for %s. kolk rotates to another free model on its own when one is available.",
			roundedWait(httpErr.RetryAfter))
	}
	return Advice{
		Summary:    "rate-limited by the provider",
		NextAction: next,
		RetryAfter: httpErr.RetryAfter,
	}
}

// roundedWait says "42 seconds" rather than "42s", because this line is read
// mid-sentence by someone who is already annoyed.
func roundedWait(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d seconds", int(d.Round(time.Second).Seconds()))
	}
	return fmt.Sprintf("%d minutes", int(d.Round(time.Minute).Minutes()))
}

func adviseTransport(err error) (Advice, bool) {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return Advice{
			Summary:    "could not reach the provider",
			NextAction: "Check the network. A proxy, a VPN, or a `--base-url` pointing at something that is not running are the usual causes.",
		}, true
	}
	// A stream that stops early is not a refusal: nothing said no, the bytes
	// just ran out. It is worth naming, because the partial answer on screen
	// makes it look like kolk decided to stop.
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "unexpected eof") || strings.Contains(text, "connection reset") ||
		strings.Contains(text, "broken pipe") {
		return Advice{
			Summary:    "the connection dropped part-way through the answer",
			NextAction: "What arrived before the break is kept. Ask again to continue.",
		}, true
	}
	return Advice{}, false
}
