//go:build !windows

package shell

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A killed command must take everything it started with it.
//
// Without a process group, `npm test &` or any shell pipeline survives a
// cancelled turn, and in a long-lived daemon those orphans accumulate until
// something runs out of file descriptors. This is the test that makes Setpgid
// in exec_unix.go load-bearing rather than decorative.
func TestTimeoutKillsTheWholeProcessGroup(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")

	// Start a grandchild in the background, record its pid, then block. The
	// grandchild outlives the shell unless the whole group is signalled.
	script := fmt.Sprintf("sleep 30 & echo $! > %s; wait", pidFile)

	start := time.Now()
	res, err := New().Run(context.Background(), Cmd{Command: script, Timeout: 300 * time.Millisecond})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.TimedOut {
		t.Fatalf("the command did not time out: %+v", res)
	}

	// Returning promptly is half the property, and the half that is easy to
	// lose. An orphaned grandchild inherits the output pipe, so CombinedOutput
	// blocks until IT exits — with the group kill removed, this same test
	// still passes, thirty seconds later, because the sleep finished on its
	// own. A timeout that takes 100x the timeout is not a timeout.
	if elapsed > 5*time.Second {
		t.Errorf("Run took %s for a 300ms timeout; something is still holding the output pipe", elapsed)
	}

	pid := readPID(t, pidFile)
	if pid <= 0 {
		t.Skip("the grandchild never recorded its pid; nothing to assert")
	}

	// Give the signal a moment to be delivered and reaped.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !alive(pid) {
			return // the group died with its leader
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Do not leave a stray process behind for whoever runs the tests next.
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Errorf("grandchild %d survived the timeout; the process group was not killed", pid)
}

// A successful shell can exit while an intentional background list still
// owns stdout. CombinedOutput otherwise waits for that last writer forever:
// the command timeout no longer helps after the direct child has exited.
// This is the exact shape used by local mock-server rehearsals.
func TestSuccessfulBackgroundListCannotFreezeOutputCapture(t *testing.T) {
	dir := t.TempDir()
	script := fmt.Sprintf(
		"cd %q && nohup sleep 3 > background.log 2>&1 & sleep 0.05; echo foreground-finished",
		dir,
	)

	start := time.Now()
	res, err := New().Run(context.Background(), Cmd{Command: script, Timeout: 5 * time.Second})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("successful foreground command waited %s for a background output writer", elapsed)
	}
	if !res.OK() || res.TimedOut {
		t.Fatalf("background detachment became a command failure: %+v", res)
	}
	if !strings.Contains(res.Output, "foreground-finished") {
		t.Fatalf("foreground output was lost: %q", res.Output)
	}
	if !strings.Contains(res.Output, "background process kept command output open") {
		t.Fatalf("the model was not told why output capture detached: %q", res.Output)
	}
}

func readPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(path)
		if err == nil {
			if pid, convErr := strconv.Atoi(strings.TrimSpace(string(b))); convErr == nil {
				return pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return 0
}

// alive reports whether a pid still exists. Signal 0 performs the permission
// and existence checks without actually sending anything.
func alive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}
