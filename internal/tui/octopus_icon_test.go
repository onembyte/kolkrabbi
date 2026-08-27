package tui

import (
	"strings"
	"testing"
)

// The icon is four cells of quadrant blocks. If it ever grows a newline it has
// stopped being a one-row icon, which is the whole constraint it was drawn to.
func TestOctopusIconIsOneRowOfFourCells(t *testing.T) {
	if strings.Contains(OctopusIcon, "\n") {
		t.Fatalf("icon spans more than one row: %q", OctopusIcon)
	}
	if got := cellWidth(OctopusIcon); got != 4 {
		t.Fatalf("icon is %d cells wide, want 4: %q", got, OctopusIcon)
	}
	for _, r := range OctopusIcon {
		if runeCellWidth(r) != 1 {
			t.Fatalf("icon uses a non-single-width rune %q; the composer's wrap math assumes cells", r)
		}
	}
}

// Only the wheel moves. An animated logo reads as a fault rather than progress,
// and it was a wiggling three-row sprite that this replaced.
func TestActivityLineTurnsOnlyTheWheel(t *testing.T) {
	first := activityLine(0, "thinking")
	second := activityLine(1, "thinking")
	if first == second {
		t.Fatalf("wheel did not advance between frames: %q", first)
	}
	for _, line := range []string{first, second} {
		if !strings.HasPrefix(line, OctopusIcon+" ") {
			t.Fatalf("activity row lost the octopus: %q", line)
		}
		if !strings.HasSuffix(line, " thinking…") {
			t.Fatalf("activity row lost its phase: %q", line)
		}
	}
	if strings.TrimPrefix(first, OctopusIcon) == strings.TrimPrefix(second, OctopusIcon) {
		t.Fatalf("frames differ outside the wheel")
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
