package tui

import (
	"bytes"
	"strings"
	"testing"
)

// A mouse report is consumed whole. Matching only its prefix left the
// coordinates in the buffer, where they became text the user never typed —
// which is what would have happened the moment reporting was turned on.
func TestMouseReportsAreConsumedWholeAndNeverBecomeText(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  []Key
	}{
		{"left press", "\x1b[<0;12;7M", []Key{{Kind: KeyMouse, Col: 11, Row: 6}}},
		{"left release", "\x1b[<0;12;7m", nil},
		{"right press", "\x1b[<2;12;7M", nil},
		{"drag", "\x1b[<32;12;7M", nil},
		{"wheel up", "\x1b[<64;20;5M", []Key{{Kind: KeyPageUp}}},
		{"wheel down", "\x1b[<65;20;5M", []Key{{Kind: KeyPageDown}}},
		{"a click then a letter", "\x1b[<0;3;9Mx", []Key{{Kind: KeyMouse, Col: 2, Row: 8}, {Kind: KeyText, Text: "x"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := NewDecoder().Feed([]byte(tc.input))
			if len(got) != len(tc.want) {
				t.Fatalf("keys = %+v, want %+v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("key %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
	// Split across reads, as a terminal may deliver it.
	decoder := NewDecoder()
	if keys := decoder.Feed([]byte("\x1b[<0;12")); len(keys) != 0 {
		t.Fatalf("a half-read report produced %+v", keys)
	}
	if keys := decoder.Feed([]byte(";7M")); len(keys) != 1 || keys[0].Kind != KeyMouse {
		t.Fatalf("the completed report produced %+v", keys)
	}
}

// A click in the composer puts the cursor where it was clicked: the right
// line, the right column, clamped to the end of what is written there.
func TestClickingTheComposerPlacesTheCursor(t *testing.T) {
	m := New(Status{Mode: "code", Lifecycle: "ready"})
	m.SetDraft("hello world")
	width, height := 40, 10
	rows := m.viewRows(width, height, 11)
	composerRow := -1
	for i, row := range rows {
		if strings.HasPrefix(row.text, promptMarker+" hello") {
			composerRow = i
		}
	}
	if composerRow < 0 {
		t.Fatalf("no composer row in the frame:\n%s", m.View(width, height))
	}
	for _, tc := range []struct {
		col  int
		want int
	}{
		{2, 0},   // the first character
		{7, 5},   // mid-word
		{13, 11}, // past the end clamps to the end
		{0, 0},   // on the prompt marker
	} {
		got, ok := m.ComposerHit(width, height, 11, tc.col, composerRow)
		if !ok || got != tc.want {
			t.Errorf("click at column %d = %d (%v), want %d", tc.col, got, ok, tc.want)
		}
	}
	if _, ok := m.ComposerHit(width, height, 11, 5, 0); ok {
		t.Error("a click in the transcript claimed to be in the composer")
	}
}

// A wrapped draft is hit-tested line by line, and a second line's click
// lands past the first line's runes.
func TestClickingAWrappedDraftFindsTheRightLine(t *testing.T) {
	m := New(Status{Mode: "code", Lifecycle: "ready"})
	m.SetDraft(strings.Repeat("a", 30) + strings.Repeat("b", 10))
	// The caret sits at the end, so it cannot shift the first line under
	// the click being tested.
	const caret = 40
	width, height := 24, 12
	rows := m.viewRows(width, height, caret)
	first, second := -1, -1
	for i, row := range rows {
		if strings.HasPrefix(row.text, promptMarker+" a") {
			first = i
			second = i + 1
		}
	}
	if first < 0 {
		t.Fatalf("no wrapped composer rows:\n%s", m.View(width, height))
	}
	firstHit, ok := m.ComposerHit(width, height, caret, 4, first)
	if !ok || firstHit != 2 {
		t.Fatalf("first line click = %d (%v), want 2", firstHit, ok)
	}
	secondHit, ok := m.ComposerHit(width, height, caret, 4, second)
	if !ok || secondHit <= firstHit {
		t.Fatalf("second line click = %d (%v), want past the first line", secondHit, ok)
	}
}

// The controller moves the cursor on a click, and ignores one while an
// overlay owns the screen.
func TestControllerMovesTheCursorOnAClick(t *testing.T) {
	c := NewController(Status{Mode: "code", Lifecycle: "ready"}, 1024)
	c.HandleKey(Key{Kind: KeyText, Text: "hello world"})
	c.View(40, 10)
	row := composerRowIn(t, c, 40, 10)
	if effect := c.HandleKey(Key{Kind: KeyMouse, Col: 7, Row: row}); effect.Submit != "" {
		t.Fatalf("a click submitted: %#v", effect)
	}
	if got := c.Cursor(); got != 5 {
		t.Fatalf("cursor after the click = %d, want 5", got)
	}
	c.HandleKey(Key{Kind: KeyText, Text: "X"})
	if got := c.Snapshot().Draft; got != "helloX world" {
		t.Fatalf("typing after the click = %q", got)
	}
	c.RequestApproval(Approval{Action: "run it"})
	before := c.Cursor()
	c.HandleKey(Key{Kind: KeyMouse, Col: 3, Row: row})
	if c.Cursor() != before {
		t.Fatal("a click moved the cursor while an overlay was up")
	}
}

func composerRowIn(t *testing.T, c *Controller, width, height int) int {
	t.Helper()
	for i, line := range strings.Split(c.RenderView(width, height), "\n") {
		if strings.HasPrefix(line, promptMarker+" hello") {
			return i
		}
	}
	t.Fatalf("no composer row:\n%s", c.RenderView(width, height))
	return 0
}

// Mouse reporting is asked for only when it is wanted, and is always given
// back: to a vendor login that parks the frame, and to the shell at the end.
func TestMouseReportingIsTurnedOffWheneverTheFrameLetsGo(t *testing.T) {
	var out bytes.Buffer
	renderer := NewRenderer(&out)
	renderer.SetMouse(true)
	if err := renderer.Start(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), mouseOn) {
		t.Fatalf("start did not ask for mouse reports: %q", out.String())
	}
	out.Reset()
	renderer.Park()
	if !strings.Contains(out.String(), mouseOff) {
		t.Fatalf("park kept mouse reports from the child: %q", out.String())
	}
	out.Reset()
	renderer.Resume()
	if !strings.Contains(out.String(), mouseOn) {
		t.Fatalf("resume did not take mouse reports back: %q", out.String())
	}
	out.Reset()
	if err := renderer.Close(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), mouseOff) {
		t.Fatalf("close left the terminal reporting mouse: %q", out.String())
	}

	var quiet bytes.Buffer
	plain := NewRenderer(&quiet)
	if err := plain.Start(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(quiet.String(), mouseOn) {
		t.Fatalf("mouse reports were asked for without being wanted: %q", quiet.String())
	}
}
