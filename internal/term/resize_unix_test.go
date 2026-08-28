//go:build darwin || linux

package term

import (
	"os"
	"syscall"
	"testing"
	"time"
)

func TestResizeNotifierDeliversSIGWINCHOnceAndStopsCleanly(t *testing.T) {
	changes, stop := ResizeNotifier(os.Stdout)
	defer stop()

	// A burst of signals must coalesce into a single pending notification: the
	// screen repaints once with the latest size, not fifty times.
	for range 3 {
		if err := syscall.Kill(os.Getpid(), syscall.SIGWINCH); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-changes:
	case <-time.After(2 * time.Second):
		t.Fatal("SIGWINCH was not delivered as a resize notification")
	}
	select {
	case <-changes:
		// A second receive is acceptable only if a signal raced in after the
		// first drain; three signals must never produce three receives.
		select {
		case <-changes:
			t.Fatal("resize notifications were not coalesced")
		case <-time.After(100 * time.Millisecond):
		}
	case <-time.After(100 * time.Millisecond):
	}

	stop()
	stop() // idempotent: the runtime and a deferred cleanup may both call it
}
