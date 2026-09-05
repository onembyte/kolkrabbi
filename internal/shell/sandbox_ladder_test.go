//go:build darwin || linux

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

// The cancel ladder must reach grandchildren through the wrapper. Both
// enforcers exec the command in place -- sandbox-exec applies the profile and
// execs, the Landlock child installs its ruleset and execs -- so the wrapper is
// the process-group leader that becomes the shell, and Setpgid still covers
// everything the shell starts. That is a property of the wrappers, not of the
// ladder, and a future wrapper that forks instead would break it silently:
// these are the tests that would notice. Both use the `npm test &` shape from
// the plan: a background grandchild that outlives the shell unless the whole
// group is signalled.

func sandboxedGrandchild(t *testing.T) (policy *Sandbox, script, pidFile string) {
	t.Helper()
	root, temp := t.TempDir(), t.TempDir()
	pidFile = filepath.Join(root, "child.pid")
	return &Sandbox{Root: root, Temp: temp, Network: NetworkAllow},
		fmt.Sprintf("sleep 30 & echo $! > %s; wait", pidFile), pidFile
}

// The grandchild must be gone within a moment of the leader; otherwise it is
// killed here so the next test run does not inherit it, and the test fails.
func requireGrandchildGone(t *testing.T, pidFile string) {
	t.Helper()
	pid := readPID(t, pidFile)
	if pid <= 0 {
		t.Skip("the grandchild never recorded its pid; nothing to assert")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !alive(pid) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Errorf("grandchild %d survived through the sandbox wrapper; the process group was not killed", pid)
}

func TestSandboxedTimeoutKillsTheWholeProcessGroup(t *testing.T) {
	if _, err := mechanism(); err != nil {
		t.Skipf("no sandbox mechanism here: %v", err)
	}
	policy, script, pidFile := sandboxedGrandchild(t)
	start := time.Now()
	res, err := New().Run(context.Background(), Cmd{Command: script, Dir: policy.Root, Timeout: 300 * time.Millisecond, Sandbox: policy})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.TimedOut {
		t.Fatalf("the sandboxed command did not time out: %+v", res)
	}
	if elapsed > 5*time.Second {
		t.Errorf("Run took %s for a 300ms timeout; a grandchild is still holding the output pipe", elapsed)
	}
	requireGrandchildGone(t, pidFile)
}

func TestSandboxedCancelKillsTheWholeProcessGroup(t *testing.T) {
	if _, err := mechanism(); err != nil {
		t.Skipf("no sandbox mechanism here: %v", err)
	}
	policy, script, pidFile := sandboxedGrandchild(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// Let the shell start its grandchild before the turn is cancelled.
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) && readPIDQuietly(pidFile) <= 0 {
			time.Sleep(20 * time.Millisecond)
		}
		cancel()
	}()
	start := time.Now()
	_, err := New().Run(ctx, Cmd{Command: script, Dir: policy.Root, Timeout: 30 * time.Second, Sandbox: policy})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("a cancelled sandboxed command returned no error")
	}
	if elapsed > 5*time.Second {
		t.Errorf("Run took %s after cancellation; a grandchild is still holding the output pipe", elapsed)
	}
	requireGrandchildGone(t, pidFile)
}

// readPIDQuietly is readPID for a poller: zero until the file holds a pid,
// never a test failure, because the file legitimately does not exist yet.
func readPIDQuietly(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return pid
}
