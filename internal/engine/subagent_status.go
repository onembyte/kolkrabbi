package engine

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// SubagentState is the observed lifecycle vocabulary exposed to interactive
// surfaces and the durable work ledger. It deliberately describes facts, not
// guessed progress: there is no percentage or ETA state.
type SubagentState string

const (
	SubagentQueued  SubagentState = "queued"
	SubagentWaiting SubagentState = "waiting"
	SubagentWorking SubagentState = "working"
	SubagentDone    SubagentState = "done"
	SubagentFailed  SubagentState = "failed"
	SubagentBlocked SubagentState = "blocked"
)

// SubagentPhase says which part of a task owns its latest observed step. The
// vocabulary is intentionally broad enough for every provider: provider-owned
// tools and Kolkrabbi-owned tools both map to tool.
type SubagentPhase string

const (
	SubagentPhaseSchedule   SubagentPhase = "schedule"
	SubagentPhaseProvider   SubagentPhase = "provider"
	SubagentPhaseTool       SubagentPhase = "tool"
	SubagentPhaseCheckpoint SubagentPhase = "checkpoint"
	SubagentPhaseComplete   SubagentPhase = "complete"
)

// SubagentStatus is one presentation-safe task update. Sequence is monotonic
// within the task and lets concurrent consumers reject stale replacements.
// Step is a bounded, whitespace-folded preview; complete provider output stays
// in the task report and event journal instead of entering this row.
type SubagentStatus struct {
	ID        string
	ChildTurn string
	Index     int
	Total     int
	Model     string
	Effort    string
	Summary   string
	State     SubagentState
	Phase     SubagentPhase
	Step      string
	Sequence  uint64
}

const maxSubagentStepRunes = 160

// advanceSubagentStatus applies one observed transition without mutating the
// input on failure. Repeated non-terminal states are valid: several provider
// or tool steps can happen while a task remains working.
func advanceSubagentStatus(current SubagentStatus, state SubagentState, phase SubagentPhase, step string) (SubagentStatus, error) {
	if !validSubagentState(state) {
		return current, fmt.Errorf("unknown subagent state %q", state)
	}
	if !validSubagentPhase(phase) {
		return current, fmt.Errorf("unknown subagent phase %q", phase)
	}
	if (state == SubagentDone || state == SubagentFailed || state == SubagentBlocked) != (phase == SubagentPhaseComplete) {
		return current, fmt.Errorf("subagent state %q is incompatible with phase %q", state, phase)
	}
	step = compactSubagentStep(step)
	if step == "" {
		return current, fmt.Errorf("subagent step must be non-empty")
	}
	if !validSubagentTransition(current.State, state) {
		return current, fmt.Errorf("invalid subagent transition %q -> %q", current.State, state)
	}

	next := current
	next.State = state
	next.Phase = phase
	next.Step = step
	next.Sequence++
	return next, nil
}

func validSubagentState(state SubagentState) bool {
	switch state {
	case SubagentQueued, SubagentWaiting, SubagentWorking, SubagentDone, SubagentFailed, SubagentBlocked:
		return true
	default:
		return false
	}
}

func validSubagentPhase(phase SubagentPhase) bool {
	switch phase {
	case SubagentPhaseSchedule, SubagentPhaseProvider, SubagentPhaseTool, SubagentPhaseCheckpoint, SubagentPhaseComplete:
		return true
	default:
		return false
	}
}

func validSubagentTransition(from, to SubagentState) bool {
	switch from {
	case "":
		return to == SubagentQueued || to == SubagentWorking
	case SubagentQueued:
		return to == SubagentQueued || to == SubagentWaiting || to == SubagentWorking ||
			to == SubagentFailed || to == SubagentBlocked
	case SubagentWaiting:
		return to == SubagentWaiting || to == SubagentWorking || to == SubagentFailed || to == SubagentBlocked
	case SubagentWorking:
		return to == SubagentWorking || to == SubagentDone || to == SubagentFailed
	default:
		return false // terminal states never reopen
	}
}

func compactSubagentStep(value string) string {
	value = strings.Join(strings.Fields(stripSubagentControls(value)), " ")
	runes := []rune(value)
	if len(runes) <= maxSubagentStepRunes {
		return value
	}
	return string(runes[:maxSubagentStepRunes-1]) + "…"
}

// stripSubagentControls removes terminal control input before a concurrent
// producer can hand it to any surface. CSI and OSC sequences are consumed as
// units so an ANSI colour does not degrade into visible "[31m" text.
func stripSubagentControls(value string) string {
	var safe strings.Builder
	for index := 0; index < len(value); {
		if value[index] == 0x1b {
			index = skipSubagentEscape(value, index)
			continue
		}
		if value[index] < 0x20 || value[index] == 0x7f {
			safe.WriteByte(' ')
			index++
			continue
		}
		r, size := utf8.DecodeRuneInString(value[index:])
		if r == utf8.RuneError && size == 1 {
			safe.WriteRune(utf8.RuneError)
			index++
			continue
		}
		safe.WriteRune(r)
		index += size
	}
	return safe.String()
}

func skipSubagentEscape(value string, start int) int {
	next := start + 1
	if next >= len(value) {
		return next
	}
	switch value[next] {
	case '[': // CSI: parameters/intermediates followed by one final byte.
		next++
		for next < len(value) {
			final := value[next]
			next++
			if final >= 0x40 && final <= 0x7e {
				break
			}
		}
		return next
	case ']': // OSC: BEL or ST terminates the string.
		next++
		for next < len(value) {
			if value[next] == 0x07 {
				return next + 1
			}
			if value[next] == 0x1b && next+1 < len(value) && value[next+1] == '\\' {
				return next + 2
			}
			next++
		}
		return next
	default:
		for next < len(value) && value[next] >= 0x20 && value[next] <= 0x2f {
			next++
		}
		if next < len(value) {
			next++
		}
		return next
	}
}
