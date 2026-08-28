package engine

import (
	"fmt"
	"strings"
)

// What a session does when free models cannot serve it — at startup, because
// the catalogue lists no free tool-capable model, or mid-run, because every
// free model it can reach is rate-limited.
//
// The vocabulary is deliberately not the one `routing.on_subscription_limit`
// uses (A33.7). That setting answers "your plan ran out, may I bill you?", and
// `ask` is a sensible default because a person is usually there when a
// subscription lapses. This one answers "free ran out", which happens
// mid-sentence and repeatedly, and a prompt every time free models rate-limit
// would be an interruption rather than a decision. What the two share is the
// rule that matters: the default never bills.
const (
	// OnFreeExhaustedFree stays free. Free models rotate among themselves and
	// the run stops rather than moving to a billed one. The default.
	OnFreeExhaustedFree = "free"
	// OnFreeExhaustedPaid allows the move to a metered model, saying so.
	OnFreeExhaustedPaid = "paid"
	// OnFreeExhaustedStop refuses to substitute anything at all — for someone
	// who would rather see an error than a session quietly on another model.
	OnFreeExhaustedStop = "stop"
)

// NormalizeFreeExhausted turns configured text into one of the three policies.
// Empty means unset, which is free.
func NormalizeFreeExhausted(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return OnFreeExhaustedFree, nil
	case OnFreeExhaustedFree:
		return OnFreeExhaustedFree, nil
	case OnFreeExhaustedPaid:
		return OnFreeExhaustedPaid, nil
	case OnFreeExhaustedStop:
		return OnFreeExhaustedStop, nil
	default:
		return "", fmt.Errorf("unknown policy %q: use free, paid or stop", strings.TrimSpace(value))
	}
}
