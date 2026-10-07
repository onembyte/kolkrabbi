//go:build !windows

package shell

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// "Never started" is the one proof that a prompt never reached a vendor, so
// it is given exactly when nothing ran: an executable that is missing, or one
// the system refuses to start. A process that ran and failed may have acted
// on what it was given, and must never be reported as unstarted.
func TestRunLinesSaysWhenAProcessNeverRan(t *testing.T) {
	dir := t.TempDir()
	unrunnable := filepath.Join(dir, "unrunnable")
	// Executable, but not a program: no interpreter line, no binary format.
	if err := os.WriteFile(unrunnable, []byte("not a program\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	failing := filepath.Join(dir, "failing")
	if err := os.WriteFile(failing, []byte("#!/bin/sh\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, executable string
		neverRan         bool
	}{
		{"missing", filepath.Join(dir, "absent"), true},
		{"unrunnable", unrunnable, true},
		{"ran and failed", failing, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := RunLinesWithOptions(context.Background(), tc.executable, nil, nil, func([]byte) error { return nil }, ProcessOptions{})
			if err == nil {
				t.Fatal("no error")
			}
			var notStarted *NotStartedError
			if errors.As(err, &notStarted) != tc.neverRan {
				t.Fatalf("error %q: never started = %v, want %v", err, !tc.neverRan, tc.neverRan)
			}
		})
	}
}

// A read that fails (here, a line past the bound) kills the child, and that
// kill is reported as one: the vendor's turn was cut off, not ended, so its
// conversation must not be continued as if it were whole.
func TestAChildKilledOnAFailedReadIsAHardExit(t *testing.T) {
	process, err := StartLinesProcess(context.Background(), "sh", []string{"-c", "head -c 17000000 /dev/zero | tr '\\0' x; sleep 30"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := process.Next(ctx); err == nil {
		t.Fatal("a line past the bound was accepted")
	}
	for deadline := time.Now().Add(5 * time.Second); !process.Exited() && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
	}
	if !process.Exited() || !process.HardExit() {
		t.Fatalf("exited %v, hard exit %v; want the kill reported", process.Exited(), process.HardExit())
	}
}
