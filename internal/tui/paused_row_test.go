package tui

import (
	"strings"
	"testing"
)

// The status line carries the session's own pause while there is one — the
// reason and when it resumes — and not a word about it otherwise.
func TestStatusLineShowsThePauseOnlyWhileThereIsOne(t *testing.T) {
	m := New(Status{Model: "vendor/pinned", Mode: "code", Effort: "medium", Sandbox: "off", Lifecycle: "ready"})
	if view := m.View(160, 24); strings.Contains(view, "paused") {
		t.Fatalf("status line mentions a pause with nothing paused:\n%s", view)
	}
	m.SetStatus(Status{
		Model: "vendor/pinned", Mode: "code", Effort: "medium", Sandbox: "off", Lifecycle: "ready",
		Paused: "paused · vendor/pinned · subscription allowance · resumes 15:04",
	})
	view := m.View(160, 24)
	for _, want := range []string{"paused", "subscription allowance", "resumes 15:04"} {
		if !strings.Contains(view, want) {
			t.Fatalf("status line omits %q while paused:\n%s", want, view)
		}
	}
}
