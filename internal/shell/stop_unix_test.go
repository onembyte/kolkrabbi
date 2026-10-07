//go:build !windows

package shell

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestSignalManagedProcessGroupStopsDetachedChild(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep unavailable")
	}
	child, err := StartDetachedProcess(context.Background(), sleep, []string{"30"}, os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Close() }()
	if err := SignalManagedProcessGroup(context.Background(), child.Pid()); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(2 * time.Second)
	for !child.Exited() {
		select {
		case <-deadline:
			t.Fatal("signalled process did not exit")
		case <-time.After(time.Millisecond):
		}
	}
}
