package tui

import (
	"strings"
	"testing"
)

func statusText(status Status) string {
	return strings.Join(formatStatus(status), "\n")
}

func TestContextAndCostAppearInTheStatusLine(t *testing.T) {
	got := statusText(Status{
		Model: "m", Effort: "medium", Lifecycle: "ready",
		Context: "42%", Cost: "$0.31",
	})

	// These are the two numbers that decide whether to compact or stop, and
	// until now the status line made someone run a command to see either.
	if !strings.Contains(got, "42%") || !strings.Contains(got, "$0.31") {
		t.Fatalf("status = %q", got)
	}
}

func TestContextAndCostComeLast(t *testing.T) {
	got := statusText(Status{
		Model: "m", Effort: "medium", Folder: "~/p", Approval: "ask", Lifecycle: "ready",
		Context: "42%", Cost: "$0.31",
	})

	// Last so a narrow terminal clips them before it clips the model or the
	// state, which someone cannot recover by running a command.
	if strings.Index(got, "42%") < strings.Index(got, "ready") {
		t.Fatalf("context came before state: %q", got)
	}
	if strings.Index(got, "$0.31") < strings.Index(got, "42%") {
		t.Fatalf("cost came before context: %q", got)
	}
}

func TestAnUnknownContextOrCostIsSimplyAbsent(t *testing.T) {
	got := statusText(Status{Model: "m", Effort: "medium", Lifecycle: "ready"})

	// Before the first turn there is nothing to report, and "context 0%" would
	// be a measurement nobody made.
	for _, unwanted := range []string{"context", "cost", "0%", "$0.00"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("status = %q, want %q absent", got, unwanted)
		}
	}
}

func TestTheStatusLineStillSanitisesThem(t *testing.T) {
	got := statusText(Status{Model: "m", Lifecycle: "ready", Cost: "$1\x1b[31m"})
	if strings.Contains(got, "\x1b[31m") {
		t.Fatalf("an escape reached the status line: %q", got)
	}
}
