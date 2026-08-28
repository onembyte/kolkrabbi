package tui

import (
	"strings"
	"testing"
)

// The wheel is the whole indicator now, so it has to actually turn.
func TestActivityLineTurnsOnlyTheWheel(t *testing.T) {
	first := activityLine(0, "thinking")
	second := activityLine(1, "thinking")
	if first == second {
		t.Fatalf("wheel did not advance between frames: %q", first)
	}
	for _, line := range []string{first, second} {
		if !strings.HasSuffix(line, " thinking…") {
			t.Fatalf("activity row lost its phase: %q", line)
		}
		// Nothing precedes the wheel: the row opens with the spinner itself.
		if !strings.HasPrefix(line, wheelFrames[0]) && !strings.HasPrefix(line, wheelFrames[1]) {
			t.Fatalf("activity row does not start with the wheel: %q", line)
		}
	}
}

// The controller reads the phase back out of this line to set the lifecycle, so
// a word it does not know would silently become "working" anyway. Say so here.
func TestActivityLineRejectsAnUnknownPhase(t *testing.T) {
	if got := activityLine(0, "Reading file — PLAN.md"); !strings.HasSuffix(got, " working…") {
		t.Fatalf("unknown phase leaked into the status row: %q", got)
	}
	if got := activityLine(0, "  PLANNING  "); !strings.HasSuffix(got, " planning…") {
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
