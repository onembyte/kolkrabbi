package tui

import (
	"context"
	"io"
	"strings"
	"testing"
)

// The activity line says what the agent is doing, not only that it is
// working: the tool it started, then whatever the engine reports next —
// ephemeral, each update replacing the last, gone when the work ends. A
// detail that arrives with nothing running shows nothing.
func TestActivityLineSaysWhatTheAgentIsDoing(t *testing.T) {
	runtime := NewRuntime(RuntimeOptions{Output: io.Discard, Status: Status{Mode: "code", Lifecycle: "thinking"}})
	runtime.spinClock = newFakeSpinnerClock()
	stop := runtime.StartWork(context.Background(), "Reading file — PLAN.md")
	if got, want := runtime.Snapshot().Activity, activityLineDetail(0, "working", "Reading file — PLAN.md"); got != want {
		t.Fatalf("activity = %q, want %q", got, want)
	}
	runtime.WorkDetail("model is responding")
	if got := runtime.Snapshot().Activity; !strings.Contains(got, "model is responding") || strings.Contains(got, "Reading file") {
		t.Fatalf("a newer detail did not replace the older: %q", got)
	}
	stop()
	if got := runtime.Snapshot().Activity; got != "" {
		t.Fatalf("activity after the work ended = %q", got)
	}
	runtime.WorkDetail("late")
	if got := runtime.Snapshot().Activity; got != "" {
		t.Fatalf("a detail with nothing running showed %q", got)
	}
}
