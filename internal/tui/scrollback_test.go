package tui

import (
	"fmt"
	"strings"
	"testing"
)

// The reported symptom from agent mode: output "printing upwards". The frame is
// repainted in place, so before this every new line shifted the rest up a row
// and the top one was overwritten -- gone, not scrolled. Anything that leaves
// the frame must now be written out first, which is what puts it in the
// terminal's scrollback.
func TestOutputThatLeavesTheFrameIsCommittedNotOverwritten(t *testing.T) {
	var out strings.Builder
	renderer := NewRenderer(&out)
	controller := NewController(Status{Mode: "agent"}, defaultDraftSize)

	const width, height = 40, 10
	for line := range 60 {
		controller.AppendTranscript(fmt.Sprintf("subagent line %d\n", line))
		committed := controller.CommitOverflow(width, height)
		if err := renderer.Render(committed, controller.RenderView(width, height)); err != nil {
			t.Fatal(err)
		}
	}

	written := out.String()
	// Every line either scrolled into history or is still on screen. None may
	// have been silently overwritten.
	for line := range 60 {
		if !strings.Contains(written, fmt.Sprintf("subagent line %d", line)) {
			t.Fatalf("line %d never reached the terminal: it was overwritten in place", line)
		}
	}
}

// A line must not be printed to scrollback while it is still on screen, or the
// reader sees it twice.
func TestACommittedLineIsNoLongerInTheFrame(t *testing.T) {
	controller := NewController(Status{Mode: "agent"}, defaultDraftSize)
	const width, height = 40, 10
	for line := range 40 {
		controller.AppendTranscript(fmt.Sprintf("line %d\n", line))
	}
	committed := controller.CommitOverflow(width, height)
	if len(committed) == 0 {
		t.Fatal("nothing was committed from a transcript four times the screen")
	}
	// Compare whole rows: "line 3" is a substring of "line 30", and matching
	// loosely would report a duplicate that is not there.
	onScreen := map[string]bool{}
	for _, row := range strings.Split(stripANSI(controller.RenderView(width, height)), "\n") {
		onScreen[strings.TrimRight(row, " ")] = true
	}
	for _, line := range committed {
		row := strings.TrimRight(stripANSI(line), " ")
		if row == "" {
			continue
		}
		if onScreen[row] {
			t.Errorf("committed row %q is still in the frame, so it shows twice", row)
		}
	}
}

// Committing must never cut a code block in half: the part that goes to
// scrollback has to look exactly as it did on screen.
func TestCommittingNeverSplitsACodeBlock(t *testing.T) {
	controller := NewController(Status{Mode: "agent"}, defaultDraftSize)
	const width, height = 40, 12
	controller.AppendTranscript(strings.Repeat("prose line\n", 30))
	controller.AppendTranscript("```go\nfunc main() {\n\tprintln(1)\n}\n```\n")
	controller.AppendTranscript(strings.Repeat("more prose\n", 30))

	var committed []string
	for range 5 {
		committed = append(committed, controller.CommitOverflow(width, height)...)
	}
	joined := stripANSI(strings.Join(committed, "\n"))
	opens := strings.Count(joined, "╭─")
	closes := strings.Count(joined, "╰─")
	if opens != closes {
		t.Errorf("committed %d code block tops and %d bottoms: a block was cut in half", opens, closes)
	}
}

// Nothing is committed while it all still fits, so a short session's output is
// never duplicated between scrollback and the frame.
func TestNothingIsCommittedWhileItAllFits(t *testing.T) {
	controller := NewController(Status{Mode: "chat"}, defaultDraftSize)
	controller.AppendTranscript("one\ntwo\nthree\n")
	if committed := controller.CommitOverflow(80, 40); committed != nil {
		t.Errorf("committed %q from a transcript that fits on screen", committed)
	}
}

// A caller that asks for no height wants everything, and must not have the
// transcript cut out from under it.
func TestAnUnboundedHeightCommitsNothing(t *testing.T) {
	controller := NewController(Status{Mode: "chat"}, defaultDraftSize)
	controller.AppendTranscript(strings.Repeat("line\n", 500))
	if committed := controller.CommitOverflow(80, 0); committed != nil {
		t.Errorf("committed %d lines when no height was given", len(committed))
	}
}

func stripANSI(text string) string {
	var out strings.Builder
	for index := 0; index < len(text); {
		if text[index] == 0x1b {
			for index < len(text) && text[index] != 'm' {
				index++
			}
			index++
			continue
		}
		out.WriteByte(text[index])
		index++
	}
	return out.String()
}
