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
	assertOrdered(t, view,
		"assistant first response",
		"🐙 thinking…",
		"kolk-code",
		"> fix the renderer",
		"  then run every test  ",
		"session 01TESTSESSION · model stealth/ox-alpha",
		"effort ultra · approval ask · state ready",
	)
	if !strings.Contains(view, "approval ask") {
		t.Fatalf("composer footer omitted approval state:\n%s", view)
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
	if !strings.Contains(frame[0], "kolk-code") || !strings.HasPrefix(frame[0], "─") ||
		!strings.HasSuffix(frame[0], "─") || cellWidth(frame[0]) != 64 {
		t.Fatalf("opening rule is not a full-width labeled line: %q", frame[0])
	}
	if frame[1] != "> continue carefully" {
		t.Fatalf("input row = %q, want a text-only prompt", frame[1])
	}
	if frame[2] != strings.Repeat("─", 64) {
		t.Fatalf("closing rule = %q", frame[2])
	}
	if frame[3] != "session 20260824-061500-abcd · model openrouter/free" {
		t.Fatalf("primary status row = %q", frame[3])
	}
	if frame[4] != "effort ultra · approval auto · state ready" {
		t.Fatalf("secondary status row = %q", frame[4])
	}
	for _, decorative := range []string{"╭", "╰", "│", "❯", "✦", "⚡", "▸", "⏵", "🐙"} {
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
	if !strings.Contains(view, "kolk-code · ~/kolkrabbi") {
		t.Fatalf("composer title omitted the working folder:\n%s", view)
	}
	for _, want := range []string{
		"session fix detached output · model cohere/north-mini-code:free",
		"effort ultra · folder ~/kolkrabbi · approval auto · state working",
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
	if !strings.Contains(view, "> next request\n"+strings.Repeat("─", 30)+"\nmodel model\nstate ready") {
		t.Fatalf("transcript displaced the composer:\n%s", view)
	}
}

func TestViewWrapsDraftWithoutChangingStoredDraft(t *testing.T) {
	m := New(Status{Mode: "code"})
	m.SetDraft("abcdefghijklmnop")

	view := m.View(12, 8)
	for _, want := range []string{"> abcdefghij", "  klmnop"} {
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
		"> 🐙abcd\n  e",
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
	for _, want := range []string{"assistant safe", "🐙 thinking…", "spoof", "kolk-code spoof", "model model", "keepthis"} {
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
