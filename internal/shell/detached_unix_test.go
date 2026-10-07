//go:build !windows

package shell

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestDetachedRuntimeSurvivesParentExit(t *testing.T) {
	if dir := os.Getenv("KOLK_DETACH_TEST_DIR"); dir != "" {
		// Only the outer test writes release, after this launcher exited.
		// A sleep alone is insufficient under race's delayed process exit.
		_, err := StartDetachedProcess(context.Background(), "sh", []string{"-c",
			`i=0; while [ "$i" -lt 200 ]; do if [ -f "$KOLK_DETACH_TEST_DIR/release" ]; then printf survived > "$KOLK_DETACH_TEST_DIR/result"; exit; fi; i=$((i+1)); sleep 0.02; done`},
			[]string{"PATH=/usr/bin:/bin", "KOLK_DETACH_TEST_DIR=" + dir})
		if err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDetachedRuntimeSurvivesParentExit$")
	child.Env = append(os.Environ(), "KOLK_DETACH_TEST_DIR="+dir)
	if out, err := child.CombinedOutput(); err != nil {
		t.Fatalf("launcher: %v %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		if out, err := os.ReadFile(filepath.Join(dir, "result")); err == nil && string(out) == "survived" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("runtime did not survive its launcher's exit")
}

func TestManagedCloseDoesNotSignalAnAlreadyReapedPID(t *testing.T) {
	finished, err := StartManagedProcess(context.Background(), "true", nil, []string{"PATH=/usr/bin:/bin"})
	if err != nil {
		t.Fatal(err)
	}
	waitErr := <-finished.done
	finished.done <- waitErr
	victim, err := StartManagedProcess(context.Background(), "sleep", []string{"10"}, []string{"PATH=/usr/bin:/bin"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = victim.Close() }()
	// Model PID reuse with a second process wholly owned by this test.
	finished.cmd.Process = victim.cmd.Process
	if err := finished.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-victim.done:
		victim.done <- err
		t.Fatal("close signalled a different process after the original was reaped")
	case <-time.After(40 * time.Millisecond):
	}
}
