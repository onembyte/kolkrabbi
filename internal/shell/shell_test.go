package shell

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunCapturesOutput(t *testing.T) {
	res, err := New().Run(context.Background(), Cmd{Command: echo("hello_kolk")})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(res.Output, "hello_kolk") {
		t.Errorf("Output = %q, want it to contain hello_kolk", res.Output)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
	if !res.OK() {
		t.Errorf("Failure = %q, want a successful result", res.Failure)
	}
}

// stderr must come back too. A tool result that silently drops the error
// message is worse than no tool result: the model retries the same command.
func TestRunInterleavesStderr(t *testing.T) {
	cmd := "echo out; echo err 1>&2"
	if runtime.GOOS == "windows" {
		cmd = "Write-Output out; [Console]::Error.WriteLine('err')"
	}
	res, err := New().Run(context.Background(), Cmd{Command: cmd})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, want := range []string{"out", "err"} {
		if !strings.Contains(res.Output, want) {
			t.Errorf("Output = %q, missing %q", res.Output, want)
		}
	}
}

func TestRunReportsAFailingExitCode(t *testing.T) {
	res, err := New().Run(context.Background(), Cmd{Command: "exit 3"})
	if err != nil {
		t.Fatalf("a non-zero exit is a result, not an error from Run: %v", err)
	}
	if res.ExitCode != 3 {
		t.Errorf("ExitCode = %d, want 3", res.ExitCode)
	}
	if res.OK() {
		t.Error("Failure should describe the exit so a caller can report it")
	}
}

func TestRunHonoursTheWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := "ls"
	if runtime.GOOS == "windows" {
		cmd = "Get-ChildItem -Name"
	}
	res, err := New().Run(context.Background(), Cmd{Command: cmd, Dir: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(res.Output, "marker.txt") {
		t.Errorf("Output = %q, the command did not run in Dir", res.Output)
	}
}

func TestRunPassesExtraEnv(t *testing.T) {
	cmd := "echo $KOLK_TEST_VAR"
	if runtime.GOOS == "windows" {
		cmd = "Write-Output $env:KOLK_TEST_VAR"
	}
	res, err := New().Run(context.Background(), Cmd{Command: cmd, Env: []string{"KOLK_TEST_VAR=set-by-test"}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(res.Output, "set-by-test") {
		t.Errorf("Output = %q, the extra environment did not reach the command", res.Output)
	}
	// The rest of the environment must survive: replacing it would break every
	// command that needs PATH, which is nearly all of them.
	res, err = New().Run(context.Background(), Cmd{Command: pathEcho(), Env: []string{"KOLK_TEST_VAR=x"}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.TrimSpace(res.Output) == "" {
		t.Error("PATH was lost when Env was set; the child got a bare environment")
	}
}

func TestRunDoesNotInheritCredentialEnvironment(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "shell-env-canary")
	cmd := "echo $OPENROUTER_API_KEY"
	if runtime.GOOS == "windows" {
		cmd = "Write-Output $env:OPENROUTER_API_KEY"
	}
	res, err := New().Run(context.Background(), Cmd{Command: cmd})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(res.Output, "shell-env-canary") {
		t.Fatalf("credential environment reached the shell: %q", res.Output)
	}
}

// A timeout must say it timed out. "signal: killed" sends whoever reads it
// looking for the wrong bug.
func TestRunTimesOutWithAReadableMessage(t *testing.T) {
	start := time.Now()
	res, err := New().Run(context.Background(), Cmd{Command: sleepCmd(), Timeout: 150 * time.Millisecond})
	if err != nil {
		t.Fatalf("a timeout is a result, not an error from Run: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("took %s; the timeout was not enforced", elapsed)
	}
	if !res.TimedOut {
		t.Error("TimedOut = false; a caller cannot tell a timeout from a crash")
	}
	if !strings.Contains(res.Failure, "timed out") {
		t.Errorf("Failure = %q, want a message naming the timeout", res.Failure)
	}
}

// A cancelled turn is not a failed command, and the caller has to be able to
// tell the difference — that is what makes Ctrl+C different from a broken tool.
func TestRunReportsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	res, err := New().Run(ctx, Cmd{Command: sleepCmd()})
	if err == nil {
		t.Fatal("a cancelled command must return an error the caller can recognise")
	}
	if !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("err = %v, want a context cancellation", err)
	}
	if res.ExitCode != 130 {
		t.Errorf("ExitCode = %d, want 130 (SIGINT by shell convention)", res.ExitCode)
	}
}

func TestLookPathExplainsItself(t *testing.T) {
	_, err := LookPath("kolk-definitely-not-installed")
	if err == nil {
		t.Fatal("LookPath found a command that should not exist")
	}
	if !strings.Contains(err.Error(), "not installed") && !strings.Contains(err.Error(), "PATH") {
		t.Errorf("err = %v, should say the command is not on PATH", err)
	}
}

func TestNameReportsTheInterpreter(t *testing.T) {
	name := New().Name()
	if name == "" {
		t.Fatal("Name() is empty; kolk doctor and every error message need it")
	}
	if runtime.GOOS != "windows" && name != "bash" {
		t.Errorf("Name() = %q, want bash on this platform", name)
	}
}

// ── small platform-neutral command builders ────────────────────────────────

func echo(s string) string {
	if runtime.GOOS == "windows" {
		return "Write-Output " + s
	}
	return "echo " + s
}

func pathEcho() string {
	if runtime.GOOS == "windows" {
		return "Write-Output $env:PATH"
	}
	return "echo $PATH"
}

func sleepCmd() string {
	if runtime.GOOS == "windows" {
		return "Start-Sleep -Seconds 30"
	}
	return "sleep 30"
}
