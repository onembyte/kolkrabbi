package shell

import (
	"context"
	"testing"
	"time"
)

// A line queued after the child exited provably never reached it, and Queue
// says so every time, however a select between two ready channels falls.
func TestQueueSaysALineNeverReachedAnExitedChild(t *testing.T) {
	process, err := StartLinesProcess(context.Background(), "sh", []string{"-c", "exit 0"})
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	for deadline := time.Now().Add(5 * time.Second); !process.Exited() && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
	}
	if !process.Exited() {
		t.Fatal("the child never exited")
	}
	for i := 0; i < 50; i++ {
		if process.Queue([]byte("hello")) {
			t.Fatalf("attempt %d: a line was reported handed to a child that had exited", i+1)
		}
	}
}
