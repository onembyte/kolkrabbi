package tui

import (
	"strings"
	"time"
)

const spinnerInterval = 120 * time.Millisecond

// OctopusIcon is Kolkrabbi at icon size: one terminal row, four cells.
//
// Quadrant block glyphs carry two pixel rows per row of text, so the icon is
// drawn on an 8x2 grid and encoded once, by hand, rather than assembled from
// characters:
//
//	. # . # # . # .   the head, with a notch where each eye sits
//	# . # # # # . #   four arms
//
// Two pixel rows is the whole budget for a single-row icon. Eyes cannot be
// filled at that height, so they are cut out of the dome instead — the notches
// are what keep this reading as the website's octopus rather than an arch.
const OctopusIcon = "▞▟▙▚"

// wheelFrames is the braille spinner. Braille is used for motion rather than
// for the icon: it animates in one cell and every monospace font ships it.
var wheelFrames = [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// knownPhases are the words the engine reports. The controller reads the phase
// back out of the rendered line, so an unknown word must not reach it.
var knownPhases = map[string]bool{
	"thinking": true, "planning": true, "working": true,
	"synthesizing": true, "streaming": true,
}

// activityLine is the row above the composer: the icon, the turning wheel, and
// what Kolkrabbi is doing. The icon never animates — a logo that wiggles reads
// as a fault rather than as progress.
func activityLine(frame int, phase string) string {
	phase = strings.TrimSpace(strings.ToLower(phase))
	if !knownPhases[phase] {
		phase = "working"
	}
	if frame < 0 {
		frame = 0
	}
	return OctopusIcon + " " + wheelFrames[frame%len(wheelFrames)] + " " + phase + "…"
}

type spinnerTimer interface {
	C() <-chan time.Time
	Stop()
}

type spinnerClock interface {
	NewTimer(time.Duration) spinnerTimer
}

type realSpinnerClock struct{}

func (realSpinnerClock) NewTimer(delay time.Duration) spinnerTimer {
	return &realSpinnerTimer{timer: time.NewTimer(delay)}
}

type realSpinnerTimer struct{ timer *time.Timer }

func (t *realSpinnerTimer) C() <-chan time.Time { return t.timer.C }
func (t *realSpinnerTimer) Stop()               { t.timer.Stop() }

// promptEcho renders a submitted request for the transcript. The marker is the
// composer's own, so a request reads the same after it is sent as while it was
// being typed, and model.go styles any line carrying it as the user's.
func promptEcho(prompt string) string {
	prompt = strings.TrimRight(prompt, "\n")
	if prompt == "" {
		return ""
	}
	lines := strings.Split(prompt, "\n")
	for index, line := range lines {
		if index == 0 {
			lines[index] = promptMarker + " " + line
			continue
		}
		// Continuation rows align under the first, exactly as the composer
		// indents them.
		lines[index] = "  " + line
	}
	return strings.Join(lines, "\n") + "\n"
}
