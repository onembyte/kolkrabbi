package engine

import (
	"encoding/json"
	"sort"
	"strings"
)

// doomThreshold is fixed at three and does not scale with effort.
//
// Effort buys more work, not more permission to repeat the same work: a
// threshold that rose with effort would mean the larger the budget, the longer
// kolk is willing to burn it achieving nothing. Three is also the saga's
// DefaultDoomThreshold, so the two guards speak one vocabulary.
const doomThreshold = 3

// doomLoop watches one turn's tool calls for the failure that MaxRoundsFor
// cannot catch: a model that keeps working and changes nothing.
//
// The rule (docs/plan/30-doom-loop-guard.md) is that a repeat needs both
// halves — the same canonical arguments *and* the same result. Success is the
// wrong discriminator: a test that fails differently each run is progress
// because the error is moving, while a read that succeeds identically three
// times is waste. What separates progress from repetition is whether anything
// changed, and the observable form of that is the result bytes.
type doomLoop struct {
	last     string // tool + canonical arguments + result of the previous call
	repeats  int    // how many times in a row that same call has been seen
	reported bool   // whether the caller has already been told about this one
}

// observe records a settled tool call and reports whether it completes a loop.
//
// It is called after the result is known, because the result is half the rule.
// A loop is reported once: the caller decides what happens next, and a stuck
// model must not produce a prompt per round while that decision is being made.
func (d *doomLoop) observe(tool, arguments, result string) bool {
	signature := tool + "\x00" + canonicalJSON(arguments) + "\x00" + result
	if signature != d.last {
		d.last = signature
		d.repeats = 1
		d.reported = false
		return false
	}
	d.repeats++
	if d.repeats < doomThreshold || d.reported {
		return false
	}
	d.reported = true
	return true
}

// reset forgets the current run. The counter belongs to a turn: asking for the
// same thing in two different turns is a person repeating themselves.
func (d *doomLoop) reset() {
	d.last = ""
	d.repeats = 0
	d.reported = false
}

// canonicalJSON re-serializes arguments with sorted keys and no insignificant
// whitespace, because providers spell the same call differently and a
// formatting difference is not a different intention.
//
// Nothing else is normalized. Not trimmed paths, not lower-cased strings, not
// "similar" arguments: an edit whose old text differs by one space is a
// different edit, and merging it with its neighbour would fire the guard on
// work that is progressing. Over-normalizing turns a safety device into a
// source of false stops, which is how safety devices get switched off.
//
// Arguments that are not valid JSON are compared as the text they are — a model
// sending the same malformed blob three times is looping too.
func canonicalJSON(arguments string) string {
	var value any
	if err := json.Unmarshal([]byte(arguments), &value); err != nil {
		return strings.TrimSpace(arguments)
	}
	var out strings.Builder
	writeCanonical(&out, value)
	return out.String()
}

func writeCanonical(out *strings.Builder, value any) {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			encoded, _ := json.Marshal(key)
			out.Write(encoded)
			out.WriteByte(':')
			writeCanonical(out, v[key])
		}
		out.WriteByte('}')
	case []any:
		out.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				out.WriteByte(',')
			}
			writeCanonical(out, item)
		}
		out.WriteByte(']')
	default:
		// Scalars, including numbers, go through the encoder so that 1 and 1.0
		// are the same argument and a string keeps its exact bytes.
		encoded, _ := json.Marshal(v)
		out.Write(encoded)
	}
}

// DoomLoopError ends a turn that has proved it is achieving nothing.
//
// It is a distinct type rather than a string because L30.3 has to name it at
// the surface with the same word the saga already uses for the same shape of
// failure, and a surface cannot match on a sentence.
type DoomLoopError struct {
	Tool    string
	Repeats int
}

func (e *DoomLoopError) Error() string {
	return "stopped: " + e.Tool + " was called " + itoa(e.Repeats) +
		" times in a row with the same arguments and the same result, so the turn was going nowhere"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
