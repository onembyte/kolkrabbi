package tui

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
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

// Reported from a 125x57 window: the composer sat near the top with the rest of
// the screen empty, and resizing made it jump upward. The frame was only as
// tall as its content, so it was not anchored to anything -- and a terminal adds
// its new rows below, not above.
func TestTheFrameFillsTheTerminalSoTheComposerStaysAtTheBottom(t *testing.T) {
	controller := NewController(Status{Mode: "code", Lifecycle: "ready"}, defaultDraftSize)
	for _, height := range []int{8, 24, 57, 80} {
		lines := strings.Split(controller.View(80, height), "\n")
		if len(lines) != height {
			t.Errorf("a %d-row terminal got a %d-row frame: the composer is not at the bottom",
				height, len(lines))
		}
	}
}

// Adding output must not move the composer. Before the fix it climbed from the
// top of the screen to the bottom as the transcript grew past the fold, so the
// layout shifted under the reader while they were using it.
func TestTheComposerDoesNotMoveAsOutputArrives(t *testing.T) {
	controller := NewController(Status{Mode: "code", Lifecycle: "ready"}, defaultDraftSize)
	const width, height = 80, 20

	position := func() int {
		lines := strings.Split(controller.View(width, height), "\n")
		for index, line := range lines {
			if strings.HasPrefix(line, "────") {
				return index
			}
		}
		return -1
	}

	first := position()
	if first < 0 {
		t.Fatal("the composer rule is not in the frame at all")
	}
	for line := range 60 {
		controller.AppendTranscript(fmt.Sprintf("output line %d\n", line))
		controller.CommitOverflow(width, height)
		if got := position(); got != first {
			t.Fatalf("after %d lines the composer moved from row %d to row %d", line+1, first, got)
		}
	}
}

// A terminal too short for the chrome must not be handed a frame taller than it
// is: that is what pushes the composer off the screen entirely.
func TestAVeryShortTerminalIsNeverGivenMoreRowsThanItHas(t *testing.T) {
	controller := NewController(Status{Mode: "code", Lifecycle: "ready"}, defaultDraftSize)
	controller.AppendTranscript(strings.Repeat("line\n", 50))
	for _, height := range []int{1, 2, 3, 4, 5} {
		lines := strings.Split(controller.View(40, height), "\n")
		if len(lines) > height {
			t.Errorf("a %d-row terminal got %d rows", height, len(lines))
		}
	}
}

// Adopted from the V43.5 §7 item 2 exercise (X1): lines reach scrollback only
// when they scroll off the top of the frame, and exit erases the frame. Whatever
// was still in it (the last answer, a block held back whole, or all of a short
// session) must be written out before the erase, not lost with it.
func TestExitLeavesTheTranscriptInTheTerminal(t *testing.T) {
	var block []string
	for line := range 10 {
		block = append(block, fmt.Sprintf("block line %d", line))
	}
	short := "\n❯ a question\n\nthe answer the user must keep\n"
	for name, c := range map[string]struct {
		width      int
		transcript string
		want       []string
	}{
		"a session that fits": {40, short, []string{"❯ a question", "the answer the user must keep"}},
		"a block across the fold": {40,
			strings.Repeat("prose line\n", 20) + "```text\n" + strings.Join(block, "\n") + "\n```\n" + "the last answer\n",
			append(append([]string{"prose line", "╭─"}, block...), "╰─", "the last answer"),
		},
		// A width the terminal cannot report is the default, as for a paint.
		"an unknown width": {0, short, []string{"❯ a question", "the answer the user must keep"}},
	} {
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer
			runtime := NewRuntime(RuntimeOptions{
				Input: bytes.NewReader([]byte("\x04")), Output: &output,
				Width: func() int { return c.width }, Height: func() int { return 12 },
				Status: Status{Model: "model", Mode: "code", Lifecycle: "ready"},
			})
			runtime.Controller().AppendTranscript(c.transcript)
			if err := runtime.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			rows := replayInline(output.String())
			screen := strings.Join(rows, "\n")
			// In order, and each block row once: shown twice is as wrong as lost.
			at := 0
			for _, want := range c.want {
				found := strings.Index(screen[at:], want)
				if found < 0 {
					t.Fatalf("after exit the terminal lost %q (or shows it out of order):\n%s", want, screen)
				}
				at += found + len(want)
				if strings.HasPrefix(want, "block line") && strings.Count(screen, want) != 1 {
					t.Errorf("%q shows %d times after exit:\n%s", want, strings.Count(screen, want), screen)
				}
			}
			// The composer and footer go: they belong to the running session.
			if strings.Contains(screen, "mode code") || strings.Contains(screen, "──────────") {
				t.Errorf("the frame's chrome survived exit:\n%s", screen)
			}
		})
	}
	// An empty session leaves nothing behind, not even a blank row.
	var output bytes.Buffer
	runtime := NewRuntime(RuntimeOptions{Input: bytes.NewReader([]byte("\x04")), Output: &output,
		Width: func() int { return 40 }, Height: func() int { return 12 }})
	if err := runtime.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if rows := replayInline(output.String()); len(rows) != 1 || rows[0] != "" {
		t.Errorf("an empty session left %q after exit", rows)
	}
}

// replayInline plays the inline renderer's stream onto an unbounded grid, the
// way a terminal's screen and scrollback end up: CR, LF, cursor up, erase to
// line end and erase below. Colour and other sequences take no cells.
func replayInline(stream string) []string {
	grid := [][]rune{{}}
	row, col := 0, 0
	savedRow, savedCol := 0, 0
	runes := []rune(stream)
	for index := 0; index < len(runes); index++ {
		switch r := runes[index]; {
		case r == '\r':
			col = 0
		case r == '\n':
			row++
			for len(grid) <= row {
				grid = append(grid, nil)
			}
		case r == 0x1b && index+1 < len(runes) && runes[index+1] == '[':
			end := index + 2
			for end < len(runes) && (runes[end] < 0x40 || runes[end] > 0x7e) {
				end++
			}
			if end == len(runes) {
				return gridRows(grid)
			}
			params := string(runes[index+2 : end])
			switch runes[end] {
			case 'A':
				n, err := strconv.Atoi(params)
				if err != nil {
					n = 1
				}
				row = max(0, row-n)
			case 'K':
				grid[row] = grid[row][:min(col, len(grid[row]))]
			case 'J':
				grid[row] = grid[row][:min(col, len(grid[row]))]
				grid = grid[:row+1]
			}
			index = end
		case r == 0x1b && index+1 < len(runes) && strings.ContainsRune("]_P", runes[index+1]):
			// String sequences (images, titles) end at BEL or ST (ESC \).
			for index += 2; index < len(runes); index++ {
				if runes[index] == 0x07 {
					break
				}
				if runes[index] == 0x1b && index+1 < len(runes) && runes[index+1] == '\\' {
					index++
					break
				}
			}
		case r == 0x1b && index+1 < len(runes):
			// Two-byte sequences: ESC 7 saves the cursor and ESC 8 restores it
			// (image placement uses them); the rest take no cells.
			switch runes[index+1] {
			case '7':
				savedRow, savedCol = row, col
			case '8':
				row, col = savedRow, savedCol
			}
			index++
		default:
			for len(grid[row]) < col {
				grid[row] = append(grid[row], ' ')
			}
			if col < len(grid[row]) {
				grid[row][col] = r
			} else {
				grid[row] = append(grid[row], r)
			}
			col++
		}
	}
	return gridRows(grid)
}

func gridRows(grid [][]rune) []string {
	rows := make([]string, len(grid))
	for index, row := range grid {
		rows[index] = strings.TrimRight(string(row), " ")
	}
	return rows
}

// Adopted from the V43.5 §7 item 2 verification (Y1): after CommitOverflow, at
// every point the fold can fall, what was committed and what remains must be
// the transcript as it renders whole, colour included, because that is what
// the terminal keeps on exit. A record cut after its heading lost its colour.
// A blank line of output is the indent alone, as workExcerpt writes it.
func TestCommittedAndRemainingRowsRenderAsTheWhole(t *testing.T) {
	record := "\n• Failed make · kolk\n  └ × command failed\n  └ line one\n    line two\n    line three\n    \n    line five\n    [exit error: exit status 2]\n  └ … 2 more lines\n\nafter the record\n"
	for prose := range 12 {
		transcript := strings.Repeat("prose line\n", prose) + record
		whole := NewController(Status{Mode: "code"}, defaultDraftSize)
		whole.AppendTranscript(transcript)
		want := whole.Remaining(40)

		split := NewController(Status{Mode: "code"}, defaultDraftSize)
		split.AppendTranscript(transcript)
		got := append(split.CommitOverflow(40, 12), split.Remaining(40)...)
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("after %d prose lines, committed + remaining differ from the whole:\n%q\nwant\n%q", prose, got, want)
		}
	}
	// Only a record is held whole: indented prose outside one, such as an
	// answer's indented code, still commits line by line as it scrolls.
	indented := NewController(Status{Mode: "code"}, defaultDraftSize)
	indented.AppendTranscript("an answer with indented code:\n" + strings.Repeat("    x := 1\n", 30))
	if committed := indented.CommitOverflow(40, 12); len(committed) < 20 {
		t.Errorf("indented prose committed %d rows of 31, want all that left the frame", len(committed))
	}
}

// The replay the exit test relies on has to model what image placement
// sends: a saved and restored cursor, and string sequences that take no cells.
func TestReplayInlineModelsCursorSaveAndStringSequences(t *testing.T) {
	got := replayInline("ab\x1b7cd\x1b8X\x1b_Gf=100;AAAA\x1b\\\r\nnext\x1b]0;title\x07 row")
	if want := []string{"abXd", "next row"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("replay = %q, want %q", got, want)
	}
}

// Adopted from the V43.5 §7 item 2 re-check (Y3): with no room for transcript
// (worker rows fill the frame), every paint commits what it can. A grouped
// record that grows across those paints must still keep its colour.
func TestAGrowingRecordCommittedWithNoRoomKeepsItsColour(t *testing.T) {
	parts := []string{"\n• Explored · agent 1\n  └ Read a.go\n", "  └ Read b.go\n", "  └ Read c.go\n", "\nafter the record\n"}
	whole := NewController(Status{Mode: "agent"}, defaultDraftSize)
	whole.AppendTranscript(strings.Join(parts, ""))
	want := whole.Remaining(40)

	growing := NewController(Status{Mode: "agent"}, defaultDraftSize)
	var got []string
	for _, part := range parts {
		growing.AppendTranscript(part)
		got = append(got, growing.CommitOverflow(40, 4)...)
	}
	got = append(got, growing.Remaining(40)...)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("a record grown across paints differs from the whole:\n%q\nwant\n%q", got, want)
	}
	// Only a record is held open: a finished line of prose still goes to
	// scrollback at once, or with no room it would not be seen at all.
	prose := NewController(Status{Mode: "agent"}, defaultDraftSize)
	prose.AppendTranscript("a finished line of prose\n")
	if committed := strings.Join(prose.CommitOverflow(40, 4), "\n"); !strings.Contains(committed, "a finished line of prose") {
		t.Errorf("with no room, a finished prose line was held back: committed %q", committed)
	}
}
