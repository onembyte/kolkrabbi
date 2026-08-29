//go:build !windows

package shell

import (
	"context"
	"syscall"
	"testing"
	"time"
)

// A managed process is its own group, and Close takes the group: an inference
// server forks runners, and killing the parent alone leaves them holding the
// GPU.
func TestManagedProcessCloseTakesTheWholeGroup(t *testing.T) {
	process, err := StartManagedProcess(context.Background(), "sh", []string{"-c", "sleep 30; true"}, []string{"PATH=/usr/bin:/bin"})
	if err != nil {
		t.Skipf("cannot start sh: %v", err)
	}
	pid := process.Pid()
	if pid <= 0 {
		t.Fatalf("pid = %d", pid)
	}
	if err := syscall.Kill(-pid, 0); err != nil {
		t.Fatalf("no process group %d: %v — Setpgid did not take", pid, err)
	}
	start := time.Now()
	if err := process.Close(); err != nil {
		t.Logf("close: %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("Close took %v", time.Since(start))
	}
	// The group is gone: signalling it finds nobody.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(-pid, 0) != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the process group survived Close")
}
