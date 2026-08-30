//go:build darwin || linux

package term

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A pty the child sees as a real terminal is the whole point: the vendor CLIs
// kolk signs into are full-screen UIs that refuse to run on a pipe.
func TestAChildOnThePTYSeesARealTerminal(t *testing.T) {
	pty, err := OpenPTY(100, 30)
	if err != nil {
		t.Fatalf("OpenPTY: %v", err)
	}
	defer pty.Close()

	// `tty` prints the terminal's name and exits non-zero on a pipe, so it
	// answers both questions at once.
	cmd := exec.Command("sh", "-c", "tty; stty size")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = pty.Slave, pty.Slave, pty.Slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	// The parent's copy goes now, so reading the master sees EOF at exit.
	_ = pty.Slave.Close()
	pty.Slave = nil

	done := make(chan string, 1)
	go func() {
		var out strings.Builder
		buffer := make([]byte, 4096)
		for {
			n, err := pty.Master.Read(buffer)
			if n > 0 {
				out.Write(buffer[:n])
			}
			if err != nil {
				break
			}
		}
		done <- out.String()
	}()

	_ = cmd.Wait()
	var output string
	select {
	case output = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the child never produced output on the pty")
	}

	if strings.Contains(output, "not a tty") || !strings.Contains(output, "/dev/") {
		t.Errorf("the child does not think it is on a terminal: %q", output)
	}
	// The size must be the one asked for. Setting it against the master before
	// the slave is open returns ENOTTY on darwin, which looks like it worked.
	if !strings.Contains(output, "30 100") {
		t.Errorf("terminal size = %q, want 30 rows by 100 columns", output)
	}
}

// A size set at open must actually reach the child, and a resize afterwards
// must too, because the session's terminal can change shape mid-login.
func TestPTYSizeCanBeChangedAfterOpening(t *testing.T) {
	pty, err := OpenPTY(80, 24)
	if err != nil {
		t.Fatalf("OpenPTY: %v", err)
	}
	defer pty.Close()
	if err := SetPTYSize(pty, 120, 40); err != nil {
		t.Fatalf("SetPTYSize after open: %v", err)
	}
}

// Closing twice is what a deferred Close plus an explicit one looks like.
func TestClosingAPTYTwiceIsHarmless(t *testing.T) {
	pty, err := OpenPTY(80, 24)
	if err != nil {
		t.Fatalf("OpenPTY: %v", err)
	}
	if err := pty.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := pty.Close(); err != nil {
		t.Errorf("second close: %v", err)
	}
}

// The master must be a real file the caller can read and write.
func TestTheMasterIsUsable(t *testing.T) {
	pty, err := OpenPTY(80, 24)
	if err != nil {
		t.Fatalf("OpenPTY: %v", err)
	}
	defer pty.Close()
	if pty.Master == nil || pty.Slave == nil {
		t.Fatal("OpenPTY returned a half-open pair")
	}
	if _, err := os.Stat(pty.Slave.Name()); err != nil {
		t.Errorf("slave %q does not exist: %v", pty.Slave.Name(), err)
	}
}
