package tui

import (
	"strings"
	"testing"
)

func TestLocalPlacementWarningStaysBelowTheComposer(t *testing.T) {
	const warning = "warning: local model will use CPU; choose /config set local.gpu_mode cpu to accept this"
	m := New(Status{Model: "ollama/qwen2.5-coder:7b", Mode: "code", Approval: "ask", LocalWarning: warning})
	m.SetDraft("keep working")
	m.SetActivity("🐙 thinking…")
	m.AppendTranscript("a long conversation\n")
	for _, size := range []struct{ width, height int }{{100, 24}, {55, 5}, {48, 4}} {
		view := m.View(size.width, size.height)
		lines := strings.Split(view, "\n")
		if !strings.Contains(lines[len(lines)-1], "warning: local model will use CPU") {
			t.Fatalf("%dx%d: warning is not the last footer row:\n%s", size.width, size.height, view)
		}
		if !strings.Contains(view, "❯ keep working") {
			t.Fatalf("%dx%d: warning displaced the composer:\n%s", size.width, size.height, view)
		}
	}
	m.SetStatus(Status{Model: "ollama/qwen2.5-coder:7b", LocalWarning: warning, Paused: "paused until reset"})
	if got := m.View(60, 5); !strings.Contains(got, "warning: local model will use CPU") || !strings.Contains(got, "paused until reset") ||
		!strings.Contains(strings.Split(got, "\n")[4], "warning: local model will use CPU") {
		t.Fatalf("pause displaced the warning or vice versa:\n%s", got)
	}
	m.SetStatus(Status{Model: "ollama/qwen2.5-coder:7b"})
	if got := m.View(100, 10); strings.Contains(got, "warning: local model") {
		t.Fatalf("warning remained after the choice changed:\n%s", got)
	}
}
