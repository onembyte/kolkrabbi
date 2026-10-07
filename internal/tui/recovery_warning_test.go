package tui

import (
	"io"
	"strings"
	"testing"
)

func TestRecoveryWarningSurvivesStatusAndTranscript(t *testing.T) {
	r := NewRuntime(RuntimeOptions{Output: io.Discard, Status: Status{Model: "test"}})
	notices, ok := any(r).(interface{ RecoveryWarning(string) })
	if !ok {
		t.Fatal("runtime cannot receive typed recovery warnings")
	}
	notices.RecoveryWarning("warning: recovery save failed; restart recovery is not guaranteed")
	r.SetStatus(Status{Model: "next", Lifecycle: "ready"})
	r.Controller().AppendTranscript(strings.Repeat("new output\n", 100))
	for _, size := range []struct{ w, h int }{{100, 24}, {70, 5}, {48, 4}} {
		view := r.controller.screen.View(size.w, size.h)
		if !strings.Contains(view, "warning: recovery save failed") {
			t.Fatalf("%dx%d notice lost: %s", size.w, size.h, view)
		}
	}
}
