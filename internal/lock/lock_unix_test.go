//go:build darwin || linux

package lock

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLockContendsAcrossProcesses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cross-process.lock")
	cmd := exec.Command(os.Args[0], "-test.run=^TestLockHelperProcess$")
	cmd.Env = append(os.Environ(),
		"KOLK_LOCK_HELPER=1",
		"KOLK_LOCK_PATH="+path,
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})

	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("waiting for helper: %v", err)
	}
	if strings.TrimSpace(line) != "locked" {
		t.Fatalf("helper said %q, want locked", line)
	}

	_, err = Try(path)
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("Try against helper = %v, want ErrBusy", err)
	}
	var busy *BusyError
	if !errors.As(err, &busy) || busy.PID != cmd.Process.Pid {
		t.Errorf("busy owner = %+v, want child PID %d", busy, cmd.Process.Pid)
	}

	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}

	held, err := Try(path)
	if err != nil {
		t.Fatalf("Try after child exit: %v", err)
	}
	if err := held.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLockHelperProcess(t *testing.T) {
	if os.Getenv("KOLK_LOCK_HELPER") != "1" {
		return
	}
	held, err := Try(os.Getenv("KOLK_LOCK_PATH"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Close() }()
	if _, err := fmt.Fprintln(os.Stdout, "locked"); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		t.Fatal(err)
	}
}
