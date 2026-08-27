package tui

import (
	"strings"
	"testing"
)

func TestControllerKeepsNextDraftWhileTheCurrentTurnStreams(t *testing.T) {
	controller := NewController(Status{
		Model: "model", Mode: "code", Effort: "standard", Session: "01SESSION",
		Approval: "ask", Lifecycle: "ready",
	}, 1024)
	controller.HandleKey(Key{Kind: KeyText, Text: "first request"})
	effect := controller.HandleKey(Key{Kind: KeyEnter})
	if effect.Submit != "first request" {
		t.Fatalf("submit effect = %#v", effect)
	}

	controller.AppendTranscript("assistant streaming ")
	controller.HandleKey(Key{Kind: KeyText, Text: "next draft"})
	controller.SetActivity("⠋")
	controller.AppendTranscript("tokens\n")

	got := controller.Snapshot()
	if got.Draft != "next draft" || got.Transcript != "assistant streaming tokens\n" {
		t.Fatalf("streaming mixed screen regions: %#v", got)
	}
	if got.Status.Lifecycle != "working" {
		t.Fatalf("lifecycle = %q, want working", got.Status.Lifecycle)
	}

	controller.FinishTurn("ready")
	got = controller.Snapshot()
	if got.Draft != "next draft" || got.Activity != "" || got.Status.Lifecycle != "ready" {
		t.Fatalf("turn completion disturbed next draft: %#v", got)
	}
}

func TestControllerFirstInterruptClearsAndSecondConsecutiveInterruptExits(t *testing.T) {
	controller := NewController(Status{Mode: "code", Lifecycle: "ready"}, 1024)
	controller.HandleKey(Key{Kind: KeyText, Text: "running"})
	controller.HandleKey(Key{Kind: KeyEnter})
	controller.HandleKey(Key{Kind: KeyText, Text: "do this next"})

	effect := controller.HandleKey(Key{Kind: KeyInterrupt})
	if effect.Interrupt || effect.Exit || controller.Snapshot().Draft != "" {
		t.Fatalf("interrupt = %#v, draft %q", effect, controller.Snapshot().Draft)
	}
	if !strings.Contains(controller.Snapshot().Activity, "Ctrl+C again") {
		t.Fatalf("first interrupt did not explain exit gesture: %#v", controller.Snapshot())
	}
	effect = controller.HandleKey(Key{Kind: KeyInterrupt})
	if effect.Interrupt || !effect.Exit {
		t.Fatalf("second consecutive interrupt = %#v, want exit only", effect)
	}
}

func TestControllerAnyOtherKeyDisarmsDoubleInterrupt(t *testing.T) {
	controller := NewController(Status{Mode: "code", Lifecycle: "ready"}, 1024)
	controller.HandleKey(Key{Kind: KeyInterrupt})
	controller.HandleKey(Key{Kind: KeyText, Text: "keep working"})
	effect := controller.HandleKey(Key{Kind: KeyInterrupt})
	if effect.Exit || controller.Snapshot().Draft != "" {
		t.Fatalf("non-consecutive interrupt exited or retained draft: %#v, %q",
			effect, controller.Snapshot().Draft)
	}
}

func TestControllerUpArrowLoadsTheLastSubmittedMessage(t *testing.T) {
	controller := NewController(Status{Mode: "code", Lifecycle: "ready"}, 1024)
	controller.HandleKey(Key{Kind: KeyText, Text: "last exact\nmessage"})
	controller.HandleKey(Key{Kind: KeyEnter})
	controller.FinishTurn("ready")
	controller.HandleKey(Key{Kind: KeyUp})
	if got := controller.Snapshot().Draft; got != "last exact\nmessage" {
		t.Fatalf("up-arrow history = %q", got)
	}
}

func TestControllerApprovalOverlayNeverConsumesTheMainDraft(t *testing.T) {
	controller := NewController(Status{Mode: "code", Lifecycle: "thinking"}, 1024)
	controller.HandleKey(Key{Kind: KeyText, Text: "next draft"})
	controller.RequestApproval(Approval{Action: "Run shell command", Detail: "go test ./..."})

	controller.HandleKey(Key{Kind: KeyText, Text: "y"})
	effect := controller.HandleKey(Key{Kind: KeyEnter})
	if effect.Decision != DecisionAllow {
		t.Fatalf("approval effect = %#v", effect)
	}
	if got := controller.Snapshot(); got.Draft != "next draft" || got.Status.Lifecycle != "thinking" {
		t.Fatalf("approval corrupted main state: %#v", got)
	}
	if controller.Approval() != nil {
		t.Fatal("answered approval remained visible")
	}
}

func TestControllerViewKeepsTheVirtualCursorInsideTheDraftAcrossOutput(t *testing.T) {
	controller := NewController(Status{Mode: "code", Lifecycle: "ready"}, 1024)
	controller.HandleKey(Key{Kind: KeyText, Text: "abcd"})
	controller.HandleKey(Key{Kind: KeyLeft})
	controller.AppendTranscript("assistant token")

	view := controller.View(40, 10)
	if want := "❯ abc▌d"; !strings.Contains(view, want) {
		t.Fatalf("view omitted cursor-bearing draft %q:\n%s", want, view)
	}
	controller.AppendTranscript(" stream")
	if view = controller.View(40, 10); !strings.Contains(view, "❯ abc▌d") {
		t.Fatalf("stream repaint moved the draft cursor:\n%s", view)
	}
}

func TestControllerRenderViewUsesPurpleOnlyForTerminalChrome(t *testing.T) {
	controller := NewController(Status{
		Model: "openrouter/free", Mode: "code", Effort: "ultra",
		SessionName: "purple TUI", Folder: "~/kolkrabbi", Lifecycle: "ready",
	}, 1024)
	controller.AppendTranscript("assistant stays plain")
	controller.HandleKey(Key{Kind: KeyText, Text: "user input stays plain"})

	plain := controller.View(100, 12)
	styled := controller.RenderView(100, 12)
	if strings.Contains(plain, "\x1b[") {
		t.Fatalf("pure view contains terminal styling: %q", plain)
	}
	for _, color := range []string{"\x1b[38;5;141m", "\x1b[38;5;103m"} {
		if !strings.Contains(styled, color) {
			t.Fatalf("styled view omitted purple palette color %q: %q", color, styled)
		}
	}
	if got := sanitizeTerminalText(styled); got != plain {
		t.Fatalf("styling changed visible text:\nplain:  %q\nstyled: %q\nstripped:%q", plain, styled, got)
	}
	for _, plainRegion := range []string{"assistant stays plain", "> user input stays plain"} {
		if strings.Contains(styled, "\x1b[38;5;141m"+plainRegion) ||
			strings.Contains(styled, "\x1b[38;5;103m"+plainRegion) {
			t.Fatalf("content region %q inherited chrome color: %q", plainRegion, styled)
		}
	}
}

func TestControllerViewSeparatesApprovalFromTheMainComposer(t *testing.T) {
	controller := NewController(Status{Mode: "code", Lifecycle: "thinking"}, 1024)
	controller.HandleKey(Key{Kind: KeyText, Text: "next draft"})
	controller.RequestApproval(Approval{Action: "Run shell command", Detail: "go test ./..."})
	controller.HandleKey(Key{Kind: KeyText, Text: "y"})

	view := controller.View(50, 12)
	for _, want := range []string{"❯ next draft▌", "Run shell command", "go test ./...", "Allow? [y/N]: y▌"} {
		if !strings.Contains(view, want) {
			t.Fatalf("approval view omitted %q:\n%s", want, view)
		}
	}
	if strings.Index(view, "❯ next draft▌") >= strings.Index(view, "Run shell command") {
		t.Fatalf("approval was not a separate focused region:\n%s", view)
	}
}

func TestControllerApprovalUsesTextOnlyHorizontalRules(t *testing.T) {
	controller := NewController(Status{Mode: "code", Lifecycle: "working"}, 1024)
	controller.RequestApproval(Approval{Action: "Run shell command", Detail: "go test ./..."})

	view := controller.View(50, 12)
	for _, want := range []string{
		horizontalRule("approval", 50),
		"Run shell command",
		"go test ./...",
		"Allow? [y/N]: ▌",
		strings.Repeat("─", 50),
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("approval omitted %q:\n%s", want, view)
		}
	}
	// Scoped to the overlay: the composer legitimately draws ❯, and the
	// footer draws ⏵. The rule under test is that the question itself is
	// plain text, not that the screen has no glyphs anywhere.
	overlay := view[strings.Index(view, horizontalRule("approval", 50)):]
	for _, decorative := range []string{"╭", "╰", "│", "❯", "✦", "⚡", "▸", "⏵", "🐙"} {
		if strings.Contains(overlay, decorative) {
			t.Fatalf("approval contains decorative token %q:\n%s", decorative, overlay)
		}
	}
}

func TestControllerShowsRecentAndLiveFilteredSlashCommands(t *testing.T) {
	controller := NewController(Status{Mode: "code", Lifecycle: "ready"}, 1024)
	controller.SetCommands([]CommandSpec{
		{Name: "mode", Usage: "/mode <name>", Summary: "switch mode"},
		{Name: "model", Usage: "/model [id]", Summary: "list or switch model"},
		{Name: "update", Usage: "/update", Summary: "install update"},
	}, 5)
	controller.RememberCommand("/update")
	controller.RememberCommand("/model old")
	controller.HandleKey(Key{Kind: KeyText, Text: "/"})

	view := controller.View(60, 12)
	assertOrdered(t, view, "/model [id]", "/update", "/mode <name>", "mode code")
	controller.HandleKey(Key{Kind: KeyText, Text: "mo"})
	view = controller.View(60, 12)
	if !strings.Contains(view, "/model [id]") || !strings.Contains(view, "/mode <name>") ||
		strings.Contains(view, "/update") {
		t.Fatalf("live prefix menu is wrong:\n%s", view)
	}
}

func TestControllerNavigatesAndCompletesAFilteredSlashCommand(t *testing.T) {
	controller := NewController(Status{Mode: "code", Lifecycle: "ready"}, 1024)
	controller.SetCommands([]CommandSpec{
		{Name: "mode", Usage: "/mode <name>", Summary: "switch mode"},
		{Name: "model", Usage: "/model [id]", Summary: "list or switch model"},
		{Name: "update", Usage: "/update", Summary: "install update"},
	}, 5)
	controller.HandleKey(Key{Kind: KeyText, Text: "/mo"})
	controller.HandleKey(Key{Kind: KeyDown})
	controller.HandleKey(Key{Kind: KeyDown})
	view := controller.View(60, 12)
	if !strings.Contains(view, "> /model [id]") {
		t.Fatalf("second filtered command was not selected:\n%s", view)
	}

	effect := controller.HandleKey(Key{Kind: KeyTab})
	if effect.Submit != "" || controller.Snapshot().Draft != "/model " {
		t.Fatalf("tab completion submitted or lost its argument space: %#v, %q",
			effect, controller.Snapshot().Draft)
	}
	if len(controller.Snapshot().Suggestions) != 0 {
		t.Fatal("completed command kept the discovery menu open")
	}
}

func TestControllerEnterCompletesOnlyAnExplicitlySelectedSuggestion(t *testing.T) {
	controller := NewController(Status{Mode: "code", Lifecycle: "ready"}, 1024)
	controller.SetCommands([]CommandSpec{{Name: "help", Usage: "/help", Summary: "commands"}}, 5)
	controller.HandleKey(Key{Kind: KeyText, Text: "/"})
	controller.HandleKey(Key{Kind: KeyDown})
	if effect := controller.HandleKey(Key{Kind: KeyEnter}); effect.Submit != "" {
		t.Fatalf("selected completion ran immediately: %#v", effect)
	}
	if got := controller.Snapshot().Draft; got != "/help" {
		t.Fatalf("selected enter completion = %q", got)
	}
	if effect := controller.HandleKey(Key{Kind: KeyEnter}); effect.Submit != "/help" {
		t.Fatalf("completed command did not submit on the next enter: %#v", effect)
	}
}
