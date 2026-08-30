//go:build darwin || linux

package shell

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

// The vendor CLIs kolk signs into are full-screen UIs that refuse to run on a
// pipe. Running them on a pty is what lets the login happen inside the session
// instead of in a window that, on a stock macOS, cannot even be opened.
func TestAChildRunInSessionGetsARealTerminal(t *testing.T) {
	var out bytes.Buffer
	err := RunInSession(context.Background(), "sh", []string{"-c", "tty; stty size"},
		strings.NewReader(""), &out, 100, 30)
	if err != nil {
		t.Fatalf("RunInSession: %v", err)
	}
	if strings.Contains(out.String(), "not a tty") || !strings.Contains(out.String(), "/dev/") {
		t.Errorf("the child does not think it is on a terminal: %q", out.String())
	}
	if !strings.Contains(out.String(), "30 100") {
		t.Errorf("the child sees size %q, want 30 rows by 100 columns", out.String())
	}
}

// Keystrokes have to reach the child, or a login prompt can never be answered.
func TestKeystrokesReachTheChild(t *testing.T) {
	var out bytes.Buffer
	err := RunInSession(context.Background(), "sh", []string{"-c", "read line; echo GOT:$line"},
		strings.NewReader("hello\n"), &out, 80, 24)
	if err != nil {
		t.Fatalf("RunInSession: %v", err)
	}
	if !strings.Contains(out.String(), "GOT:hello") {
		t.Errorf("the child never received the input: %q", out.String())
	}
}

// A child that fails must say so rather than reporting a login that did not
// happen.
func TestAFailingChildIsReported(t *testing.T) {
	var out bytes.Buffer
	err := RunInSession(context.Background(), "sh", []string{"-c", "exit 3"},
		strings.NewReader(""), &out, 80, 24)
	if err == nil {
		t.Error("a child that exited 3 was reported as a success")
	}
}

// An executable that is not installed must be named, not swallowed.
func TestAMissingExecutableIsNamed(t *testing.T) {
	var out bytes.Buffer
	err := RunInSession(context.Background(), "kolk-no-such-binary-anywhere", nil,
		strings.NewReader(""), &out, 80, 24)
	if err == nil {
		t.Fatal("a missing executable was reported as a success")
	}
}

// A cancelled turn must not leave the child running on a pty nobody is reading.
func TestACancelledContextEndsTheChild(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- RunInSession(ctx, "sh", []string{"-c", "sleep 30"}, strings.NewReader(""), &out, 80, 24)
	}()
	time.Sleep(150 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the child outlived its cancelled context")
	}
}

// The call must return when the child exits, even though nothing is ever typed:
// the keyboard pump is parked in a read that does not unblock on child exit, so
// joining it would hang until the next keystroke.
func TestItReturnsWithoutWaitingForAKeystroke(t *testing.T) {
	blocked, done := make(chan struct{}), make(chan struct{})
	var out bytes.Buffer
	go func() {
		defer close(done)
		_ = RunInSession(context.Background(), "sh", []string{"-c", "echo hi"},
			blockingReader{blocked}, &out, 80, 24)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunInSession waited for input that was never going to arrive")
	}
	close(blocked)
}

// blockingReader never returns, the way a terminal with nobody typing does not.
type blockingReader struct{ release chan struct{} }

func (r blockingReader) Read([]byte) (int, error) {
	<-r.release
	return 0, nil
}
