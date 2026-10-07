package tui

import (
	"strings"
	"testing"
)

// The icon stays emoji-sized while the wheel beside it turns.
func TestActivityLineTurnsOnlyTheWheel(t *testing.T) {
	first := activityLine(0, "thinking")
	second := activityLine(1, "thinking")
	if first == second {
		t.Fatalf("wheel did not advance between frames: %q", first)
	}
	for _, line := range []string{first, second} {
		if strings.Contains(line, "\n") || !strings.Contains(line, " thinking…") {
			t.Fatalf("activity row lost its phase: %q", line)
		}
		if !strings.HasPrefix(line, "🐙 ") || cellWidth(octopusMark) != 2 {
			t.Fatalf("activity lost its two-cell octopus: %q", line)
		}
	}
}

// The controller reads the phase back out of this line to set the lifecycle, so
// a word it does not know would silently become "working" anyway. Say so here.
func TestActivityLineRejectsAnUnknownPhase(t *testing.T) {
	if got := activityLine(0, "Reading file — PLAN.md"); !strings.Contains(got, " working…") {
		t.Fatalf("unknown phase leaked into the status row: %q", got)
	}
	if got := activityLine(0, "  PLANNING  "); !strings.Contains(got, " planning…") {
		t.Fatalf("phase was not normalised: %q", got)
	}
}

// Every frame index must be renderable: the animation loop counts up forever.
func TestActivityLineWrapsFrameIndexes(t *testing.T) {
	for frame := -3; frame < 3*len(wheelFrames); frame++ {
		if line := activityLine(frame, "working"); cellWidth(line) == 0 {
			t.Fatalf("frame %d rendered nothing", frame)
		}
	}
}
