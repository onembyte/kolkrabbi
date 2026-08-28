package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/provider"
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
// cycleWindow is how far back a repeat still counts.
//
// Consecutive identity is not the only way to achieve nothing. A model that
// alternates two calls forever — remove-and-recreate, list, remove-and-recreate,
// list — resets a consecutive counter on every call and never trips it. That is
// not hypothetical: it is what an agent-mode run did to a scaffolding task,
// alternating two calls until the turn was abandoned.
//
// To see one call repeat doomThreshold times inside a k-call rotation, the
// window has to hold k*doomThreshold calls. Nine covers rotations of one, two
// and three — the shapes a stuck model actually produces. Wider would start
// calling a legitimate read-edit-verify rhythm a loop, and a guard that stops
// real work is a guard people disable.
const cycleWindow = doomThreshold * 3

type doomLoop struct {
	last     string // tool + canonical arguments + result of the previous call
	repeats  int    // how many times in a row that same call has been seen
	reported bool   // whether the caller has already been told about this one
	denied   bool   // a subagent has been refused this call once already
	// recent is the last cycleWindow signatures, oldest first. It catches the
	// cycles `repeats` cannot see.
	recent []string
}

// observe records a settled tool call and reports whether it completes a loop.
//
// It is called after the result is known, because the result is half the rule.
// A loop is reported once: the caller decides what happens next, and a stuck
// model must not produce a prompt per round while that decision is being made.
func (d *doomLoop) observe(tool, arguments, result string) bool {
	signature := tool + "\x00" + canonicalJSON(arguments) + "\x00" + result

	d.recent = append(d.recent, signature)
	if len(d.recent) > cycleWindow {
		d.recent = d.recent[len(d.recent)-cycleWindow:]
	}

	if signature == d.last {
		d.repeats++
	} else {
		d.repeats = 1
	}
	d.last = signature

	// Either shape counts: the same call following itself, or the same call
	// coming round again inside a rotation.
	if d.repeats < doomThreshold && !d.cycling(signature) {
		// The report is only forgotten once nothing in the window is looping
		// any more. Clearing it on the next differing call would re-report the
		// same cycle on every lap, which is exactly the prompt-per-round noise
		// the single report exists to avoid.
		if !d.anyLooping() {
			d.reported = false
		}
		return false
	}
	if d.reported {
		return false
	}
	d.reported = true
	return true
}

// anyLooping reports whether anything in the window has repeated enough to
// count, not just the call that just settled.
func (d *doomLoop) anyLooping() bool {
	counts := make(map[string]int, len(d.recent))
	for _, past := range d.recent {
		counts[past]++
		if counts[past] >= doomThreshold {
			return true
		}
	}
	return false
}

// cycling reports whether this exact call has already achieved nothing
// doomThreshold times inside the window — adjacent or not.
func (d *doomLoop) cycling(signature string) bool {
	seen := 0
	for _, past := range d.recent {
		if past == signature {
			seen++
		}
	}
	return seen >= doomThreshold
}

// wouldRepeat reports whether a call that has not run yet would be the third
// identical one with, so far, identical results.
//
// The check happens before execution because the decision is that the third
// call is never made — but the result half of the rule can only come from the
// calls that already settled, which is exactly what makes two prior identical
// results the precondition. A pair that returned different bytes is not
// two-thirds of a loop.
func (d *doomLoop) wouldRepeat(tool, arguments string) bool {
	prefix := tool + "\x00" + canonicalJSON(arguments) + "\x00"
	// Two settled calls already in the window make this one the third, whether
	// or not they were adjacent — but only if they returned the SAME bytes.
	// Both halves of the rule still apply here: a command whose output keeps
	// moving is progressing, and counting it by arguments alone would stop a
	// test run that is fixing itself one failure at a time.
	seen, result := 0, ""
	for _, past := range d.recent {
		if !strings.HasPrefix(past, prefix) {
			continue
		}
		body := past[len(prefix):]
		if seen == 0 {
			result = body
		} else if body != result {
			// The results moved, so these are not repeats of one another.
			return false
		}
		seen++
	}
	if seen >= doomThreshold-1 {
		return true
	}
	if d.repeats < doomThreshold-1 {
		return false
	}
	return strings.HasPrefix(d.last, prefix)
}

// allowRepeat records that a person looked at the loop and said run it anyway.
// The count starts over, or the next identical call would ask again straight
// away and the answer would have meant nothing.
func (d *doomLoop) allowRepeat() { d.reset() }

// reset forgets the current run. The counter belongs to a turn: asking for the
// same thing in two different turns is a person repeating themselves.
func (d *doomLoop) reset() {
	d.last = ""
	d.repeats = 0
	d.reported = false
	d.recent = nil
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

// Error is deliberately short. The surface prints this line and then adds the
// detail and the next action, so a message that spelled out the whole story
// here would be read twice.
func (e *DoomLoopError) Error() string {
	return "stopped: " + e.Tool + " repeated without progress"
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

// answerDoomLoop decides what happens instead of making a call that has proved
// it changes nothing. It returns either an error that ends the turn, or the
// text to hand back as the tool's result.
//
// The tiering is item 13's, applied unchanged: it depends on who is there to
// ask. What it is emphatically not is a permission rule — `allow bash(*)`
// answers "is this dangerous?", while this answers "is this futile?", and
// collapsing the two would let a reasonable rule silently remove a spending
// guard.
func (a *Agent) answerDoomLoop(ctx context.Context, loop *doomLoop, tc provider.ToolCall, subagent bool) (string, error) {
	stop := &DoomLoopError{Tool: tc.Function.Name, Repeats: doomThreshold}

	if subagent {
		loop.allowRepeat()
		if loop.denied {
			// Told once, looped again. A child that cannot take the hint is not
			// going to recover, and it has no one to ask.
			return "", stop
		}
		loop.denied = true
		return "Error: this exact call has already been made " + itoa(doomThreshold-1) +
			" times with the same result, so it was not run again. Do something different, or stop.", nil
	}

	if a.Permission == PermissionFullAuto {
		// Full-auto never stops to ask, and there is nobody to ask. Proceeding
		// anyway is the behaviour this guard exists to prevent, so the safe
		// action is to stop — and, as everywhere else in this tier, to say what
		// happened and why.
		fmt.Fprintf(a.Out, "%s✗ stopped: %s repeated %d times with identical arguments and results (%s)%s\n",
			colorDim, tc.Function.Name, doomThreshold, compactArguments(tc.Function.Arguments), colorReset)
		return "", stop
	}

	// Someone is watching. The only escape offered is this one call: there is
	// deliberately no rule to keep, because "always allow" here would mean
	// "always allow me to spend your budget achieving nothing".
	allowed, _ := a.confirm(ctx, Confirmation{
		Action: "run " + tc.Function.Name + " again",
		Detail: "it has already run " + itoa(doomThreshold-1) + " times with the same arguments and the same result: " +
			compactArguments(tc.Function.Arguments),
	})
	if !allowed {
		return "", stop
	}
	loop.allowRepeat()
	return "", nil
}

// compactArguments keeps a doom-loop message to one line.
func compactArguments(arguments string) string {
	flat := strings.Join(strings.Fields(arguments), " ")
	if len(flat) > 120 {
		return flat[:117] + "..."
	}
	return flat
}
