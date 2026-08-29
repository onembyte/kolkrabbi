//go:build !windows

package shell

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/mockagent"
)

// shortLadder shrinks the §2.5 graces so a test can walk all three rungs
// without spending seven seconds proving arithmetic. The rungs and their order
// are what is under test; the exact seconds are a product decision recorded in
// the plan, and asserting them here would only measure time.Sleep.
func shortLadder(t *testing.T) {
	t.Helper()
	previousInterrupt, previousTerminate := sigintGrace, sigtermGrace
	sigintGrace, sigtermGrace = 150*time.Millisecond, 100*time.Millisecond
	t.Cleanup(func() { sigintGrace, sigtermGrace = previousInterrupt, previousTerminate })
}

// startMock runs one fake vendor CLI and waits until it has announced itself,
// so the signal under test cannot race the child's own startup.
func startMock(t *testing.T, ctx context.Context, kind mockagent.Kind) (*LinesProcess, string) {
	t.Helper()
	dir := t.TempDir()
	executable, logPath, err := mockagent.Write(dir, kind)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MOCKAGENT_LOG", logPath)

	process, err := StartLinesProcess(ctx, executable, nil)
	if err != nil {
		t.Fatal(err)
	}
	line, err := process.Next(context.Background())
	if err != nil {
		t.Fatalf("waiting for the child to start: %v", err)
	}
	if string(line) != "ready" {
		t.Fatalf("first line = %q, want the child's readiness announcement", line)
	}
	return process, logPath
}

// signalsReceived reads the child's own record of what reached it. Waiting is
// necessary: the handler runs after the signal is delivered, and the parent
// learns the process is gone before the log write is guaranteed visible.
func signalsReceived(t *testing.T, logPath string, want int) []string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var lines []string
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(logPath)
		if err == nil {
			lines = strings.Fields(strings.TrimSpace(string(raw)))
			if len(lines) >= want {
				return lines
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return lines
}

// §2.5 rung 1: SIGINT ends the vendor's turn gracefully AND a result frame is
// still produced, which is why the ladder must start there. Reaching for
// SIGKILL costs the turn's accounting and — per §2.5's starred rule —
// invalidates the vendor session, because a --resume afterwards silently
// continues the unfinished turn and executes tool calls kolk already told the
// user were cancelled.
func TestCancelSendsInterruptFirst(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	process, logPath := startMock(t, ctx, mockagent.ExitsOnInterrupt)
	defer func() { _ = process.Close() }()

	start := time.Now()
	cancel()

	if got := signalsReceived(t, logPath, 1); len(got) == 0 || got[0] != "INT" {
		t.Fatalf("signals = %v, want SIGINT first", got)
	}
	// A child that leaves on rung 1 must not be waited out for the whole
	// ladder: escalation is a fallback, not a schedule.
	if elapsed := time.Since(start); elapsed > sigintGrace {
		t.Fatalf("a child that exits on SIGINT took %s; the ladder is not observing its exit", elapsed)
	}
}

// Escalation has to actually happen, and has to stop at rung 2 when rung 2
// works. A ladder that jumps to SIGKILL and one that never escalates are both
// wrong, and only the child's own log can tell them apart.
func TestCancelEscalatesToTerminateButNotKill(t *testing.T) {
	shortLadder(t)
	ctx, cancel := context.WithCancel(context.Background())
	process, logPath := startMock(t, ctx, mockagent.IgnoresInterrupt)
	defer func() { _ = process.Close() }()

	cancel()

	got := signalsReceived(t, logPath, 2)
	if len(got) < 2 {
		t.Fatalf("signals = %v, want SIGINT then SIGTERM", got)
	}
	if got[0] != "INT" {
		t.Fatalf("signals = %v, want SIGINT on the first rung", got)
	}
	if got[1] != "TERM" {
		t.Fatalf("signals = %v, want SIGTERM on the second rung", got)
	}
}
