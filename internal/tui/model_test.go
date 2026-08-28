package tui

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestScreenRegionsKeepTranscriptActivityAndDraftIndependent(t *testing.T) {
	m := New(Status{
		Model:     "stealth/ox-alpha",
		Mode:      "code",
		Effort:    "ultra",
		Session:   "01TESTSESSION",
		Approval:  "ask",
		Lifecycle: "ready",
	})
	m.SetDraft("fix the renderer\nthen run every test  ")
	m.AppendTranscript("assistant first")
	m.SetActivity("🐙 thinking…")
	m.AppendTranscript(" response\n")

	got := m.Snapshot()
	if got.Transcript != "assistant first response\n" {
		t.Fatalf("transcript = %q", got.Transcript)
	}
	if got.Activity != "🐙 thinking…" {
		t.Fatalf("activity = %q", got.Activity)
	}
	if got.Draft != "fix the renderer\nthen run every test  " {
		t.Fatalf("draft changed while output arrived: %q", got.Draft)
	}

	view := m.View(80, 20)
	// The indicator now trails the first status row, below the composer, rather
	// than occupying a row above it.
	assertOrdered(t, view,
		"assistant first response",
		"❯ fix the renderer",
		"  then run every test  ",
		"⏵ ask (shift+tab) · mode code · effort ultra",
		"🐙 thinking…",
		"session 01TESTSESSION · model stealth/ox-alpha",
	)
	statusRow := ""
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "⏵ ask ") {
			statusRow = line
			break
		}
	}
	if !strings.HasSuffix(statusRow, "🐙 thinking…") {
		t.Fatalf("the indicator must sit flush right on the status row, got %q", statusRow)
	}
	// Width is measured in cells, not runes: the glyph sitting flush right is
	// one rune wide but two cells wide, and padding answers to cells.
	if cellWidth(statusRow) != 80 {
		t.Fatalf("status row is %d cells wide, want the full 80", cellWidth(statusRow))
	}
	// The tier still has to be readable without running a command; it just
	// leads its row now instead of hiding mid-sentence.
	if !strings.Contains(view, "⏵ ask ") {
		t.Fatalf("composer footer omitted the permission tier:\n%s", view)
	}
}

func TestScreenStatusChangesDoNotClearTheDraft(t *testing.T) {
	m := New(Status{Model: "old/model", Mode: "code", Effort: "standard", Lifecycle: "ready"})
	m.SetDraft("/model new/model")
	m.SetStatus(Status{
		Model:     "new/model",
		Mode:      "chat",
		Effort:    "deep",
		Session:   "01NEWSESSION",
		Approval:  "auto",
		Lifecycle: "streaming",
	})

	got := m.Snapshot()
	if got.Draft != "/model new/model" {
		t.Fatalf("status update cleared draft: %q", got.Draft)
	}
	if got.Status.Model != "new/model" || got.Status.Mode != "chat" ||
		got.Status.Lifecycle != "streaming" {
		t.Fatalf("status did not update atomically: %#v", got.Status)
	}
}

func TestViewUsesTextOnlyHorizontalComposerFrameAndLabeledStatus(t *testing.T) {
	m := New(Status{
		Model: "openrouter/free", Mode: "code", Effort: "ultra",
		Session: "20260824-061500-abcd", Approval: "auto", Lifecycle: "ready",
	})
	m.SetDraft("continue carefully")

	view := m.View(64, 10)
	lines := strings.Split(view, "\n")
	if len(lines) < 5 {
		t.Fatalf("view has no persistent composer/status region:\n%s", view)
	}
	frame := lines[len(lines)-5:]
	// The opening rule carries the mode and the name at its RIGHT end; the
	// closing rule stays unbroken. The earlier rule — neither may carry a
	// label — was about a label in the MIDDLE, which sits in front of the
	// draft; the right end is space the frame was spending anyway.
	if utf8.RuneCountInString(frame[0]) != 64 {
		t.Fatalf("opening rule is not full width: %q", frame[0])
	}
	if !strings.HasSuffix(frame[0], " code ──── kolkrabbi ─") {
		t.Fatalf("opening rule lost its right-hand label: %q", frame[0])
	}
	if frame[1] != "❯ continue carefully" {
		t.Fatalf("input row = %q, want one prompt marker and the draft", frame[1])
	}
	if frame[2] != strings.Repeat("─", 64) {
		t.Fatalf("closing rule = %q", frame[2])
	}
	if !strings.HasPrefix(frame[3], "  ⏵ auto (shift+tab) · mode code · effort ultra") {
		t.Fatalf("tier status row = %q", frame[3])
	}
	if frame[4] != "  session 20260824-061500-abcd · model openrouter/free" {
		t.Fatalf("session status row = %q", frame[4])
	}
	for _, decorative := range []string{"╭", "╰", "│", "✦", "⚡", "▸", "🐙"} {
		if strings.Contains(strings.Join(frame, "\n"), decorative) {
			t.Fatalf("static composer contains decorative token %q:\n%s", decorative, view)
		}
	}
}

func TestViewShowsSessionNameCurrentModelEffortAndWorkingFolder(t *testing.T) {
	m := New(Status{
		Model: "cohere/north-mini-code:free", Mode: "code", Effort: "ultra",
		Session: "20260824-061500-abcd", SessionName: "fix detached output",
		Folder: "~/kolkrabbi", Approval: "auto", Lifecycle: "working",
	})
	m.SetDraft("next checkpoint")

	view := m.View(160, 10)
	for _, want := range []string{
		"session fix detached output · model cohere/north-mini-code:free",
		"⏵ auto (shift+tab) · mode code · effort ultra · folder ~/kolkrabbi · state working",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("status omitted current metadata %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "session 20260824-061500-abcd") {
		t.Fatalf("session id was not replaced by its human title:\n%s", view)
	}
}

func TestStatusKeepsCoreMetadataVisibleAtTypicalTerminalWidth(t *testing.T) {
	m := New(Status{
		Model: "cohere/north-mini-code:free", Mode: "code", Effort: "ultra",
		SessionName: "purple composer checkpoint", Folder: "~/kolkrabbi",
		Approval: "auto", Lifecycle: "ready",
	})

	view := m.View(72, 10)
	for _, want := range []string{
		"session purple composer checkpoint",
		"model cohere/north-mini-code:free",
		"effort ultra",
		"folder ~/kolkrabbi",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("72-column status clipped core metadata %q:\n%s", want, view)
		}
	}
}

func TestViewKeepsComposerVisibleWhenTranscriptExceedsHeight(t *testing.T) {
	m := New(Status{Model: "model", Mode: "code", Lifecycle: "ready"})
	for i := 1; i <= 10; i++ {
		m.AppendTranscript("line-" + strconv.Itoa(i) + "\n")
	}
	m.SetDraft("next request")

	view := m.View(30, 6)
	if rows := strings.Count(view, "\n") + 1; rows > 6 {
		t.Fatalf("view used %d rows in a six-row terminal:\n%s", rows, view)
	}
	if strings.Contains(view, "line-1\n") || !strings.Contains(view, "line-10") {
		t.Fatalf("transcript viewport did not retain its newest line:\n%s", view)
	}
	if !strings.Contains(view, "❯ next request\n"+strings.Repeat("─", 30)+"\n  mode code") {
		t.Fatalf("transcript displaced the composer:\n%s", view)
	}
}

func TestViewWrapsDraftWithoutChangingStoredDraft(t *testing.T) {
	m := New(Status{Mode: "code"})
	m.SetDraft("abcdefghijklmnop")

	view := m.View(12, 8)
	for _, want := range []string{"❯ abcdefghij", "  klmnop"} {
		if !strings.Contains(view, want) {
			t.Fatalf("narrow composer omitted %q:\n%s", want, view)
		}
	}
	if got := m.Snapshot().Draft; got != "abcdefghijklmnop" {
		t.Fatalf("visual wrapping mutated submitted draft: %q", got)
	}
}

func TestViewWrapsWideAndCombiningCharactersByTerminalCells(t *testing.T) {
	m := New(Status{Mode: "code"})
	m.SetDraft("🐙abcde\ne\u0301e\u0301e\u0301e\u0301e\u0301e\u0301e\u0301")

	view := m.View(8, 12) // composer content width is six terminal cells
	for _, want := range []string{
		"❯ 🐙abcd\n  e",
		"  e\u0301e\u0301e\u0301e\u0301e\u0301e\u0301\n  e\u0301",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("cell-aware wrapping omitted %q:\n%s", want, view)
		}
	}
	if got := m.Snapshot().Draft; got != "🐙abcde\ne\u0301e\u0301e\u0301e\u0301e\u0301e\u0301e\u0301" {
		t.Fatalf("Unicode wrapping mutated draft: %q", got)
	}
}

func TestScreenStripsTerminalControlSequencesFromUntrustedRegions(t *testing.T) {
	m := New(Status{Model: "model\x1b[2J", Mode: "code\nspoof", Lifecycle: "ready"})
	m.AppendTranscript("\x1b[31massistant\x1b[0m\r\x1b[2J safe\n")
	m.SetActivity("🐙 thinking…\x1b[H\nspoof")
	m.SetDraft("keep\x1b[2Jthis")

	got := m.Snapshot()
	if got.Transcript != "assistant safe\n" {
		t.Fatalf("sanitized transcript = %q", got.Transcript)
	}
	view := m.View(60, 12)
	if strings.ContainsAny(view, "\x1b\r") {
		t.Fatalf("untrusted region retained terminal controls: %q", view)
	}
	for _, want := range []string{"assistant safe", "🐙 thinking…", "spoof", "mode code spoof", "model model", "keepthis"} {
		if !strings.Contains(view, want) {
			t.Fatalf("sanitized view omitted %q: %q", want, view)
		}
	}
}

func TestTranscriptBufferRetainsOnlyAValidUTF8Tail(t *testing.T) {
	got := appendTranscriptBounded([]byte("first\nsecond\n"), "🐙tail", 10)
	if !utf8.ValidString(string(got)) || string(got) != "d\n🐙tail" {
		t.Fatalf("bounded transcript = %q", string(got))
	}
}

func assertOrdered(t *testing.T, text string, parts ...string) {
	t.Helper()
	previous := -1
	for _, part := range parts {
		at := strings.Index(text, part)
		if at < 0 {
			t.Fatalf("view omitted %q:\n%s", part, text)
		}
		if at <= previous {
			t.Fatalf("view placed %q outside its region:\n%s", part, text)
		}
		previous = at
	}
}

func TestComposerTopRuleCarriesTheModeAndName(t *testing.T) {
	m := New(Status{Mode: "code", Lifecycle: "ready"})
	top := topRule(m.View(60, 8))

	if !strings.HasSuffix(top, " code ──── kolkrabbi ─") {
		t.Fatalf("top rule = %q, want the mode and name set into its right end", top)
	}
	if utf8.RuneCountInString(top) != 60 {
		t.Fatalf("top rule is %d cells, want the full 60", utf8.RuneCountInString(top))
	}
	if !strings.HasPrefix(top, "────") {
		t.Fatalf("label must be set into a rule, not floating: %q", top)
	}

	// The closing rule stays unbroken, so the frame still reads as a frame.
	lines := strings.Split(m.View(60, 8), "\n")
	var closing string
	for _, line := range lines {
		if strings.HasPrefix(line, "─") {
			closing = line
		}
	}
	if strings.ContainsAny(closing, "kolrabi") {
		t.Fatalf("closing rule picked up a label: %q", closing)
	}

	// A mode switch is visible without reading the footer.
	m.SetStatus(Status{Mode: "agent", Lifecycle: "ready"})
	if got := topRule(m.View(60, 8)); !strings.Contains(got, " agent ") {
		t.Fatalf("top rule did not follow the mode: %q", got)
	}

	// Too narrow for the label: a plain rule beats a clipped name.
	narrow := topRule(m.View(20, 8))
	if strings.Contains(narrow, "kolk") || utf8.RuneCountInString(narrow) != 20 {
		t.Fatalf("narrow rule = %q, want an unbroken 20-cell rule", narrow)
	}
}

// topRule is the composer's opening rule. The frame is padded to the terminal's
// full height so the composer always sits on its last row, which means the rule
// is no longer the first line of the view: find it by what it is rather than by
// where it happens to fall.
func topRule(view string) string {
	for _, line := range strings.Split(view, "\n") {
		if strings.HasPrefix(line, "────") {
			return line
		}
	}
	return ""
}
