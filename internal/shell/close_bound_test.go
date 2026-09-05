package shell

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// The one shutdown the process runner could not finish: the child leaves, but
// a grandchild it started in its own session -- outside the process group the
// kill reaches -- still holds the stdout and stderr pipes. The reader is parked
// in a read that will never return, so it never reaches Wait, so `exited` never
// closes, so Close never returns. Close must come back within a bound that does
// not depend on what the grandchild does.
func TestCloseReturnsWithinItsBoundWhenAGrandchildHoldsThePipes(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is the portable way to setsid a grandchild")
	}
	previous := closeGrace
	closeGrace = 300 * time.Millisecond
	t.Cleanup(func() { closeGrace = previous })

	process, err := StartLinesProcess(context.Background(), "sh", []string{"-c",
		`python3 -c "import os,time; os.setsid(); time.sleep(20)" & exec cat`})
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Send([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	if line, err := process.Next(context.Background()); err != nil || string(line) != "ping" {
		t.Fatalf("the child is not alive: %q %v", line, err)
	}

	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- process.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close returned %v; a close kolk asked for is not a failure", err)
		}
		if elapsed := time.Since(start); elapsed > 3*time.Second {
			t.Fatalf("Close took %s; the bound is the grace, the drain and the wait delay", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close blocked on a pipe held by a grandchild outside the process group")
	}
}
