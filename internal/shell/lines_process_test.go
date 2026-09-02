//go:build !windows

package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A persistent provider process serves every turn of a Kolkrabbi session, so
// its stderr is not a transcript worth keeping — it is a diagnostic worth
// keeping the END of. An unbounded buffer grows for as long as the session
// does, and every byte of it was being interpolated into the exit error, which
// is the string the user actually reads. A chatty vendor could therefore turn
// one failed turn into a megabyte of terminal output.
func TestLinesProcessBoundsStderrAndKeepsTheTail(t *testing.T) {
	const script = `i=0
while [ $i -lt 3000 ]; do echo "noise line $i" >&2; i=$((i+1)); done
echo "the real cause" >&2
exit 3`
	process, err := StartLinesProcess(context.Background(), "sh", []string{"-c", script})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Close() }()

	_, err = process.Next(context.Background())
	if err == nil {
		t.Fatal("a child that exits 3 must report its failure")
	}
	message := err.Error()

	if len(message) > stderrRingBytes*2 {
		t.Fatalf("exit error is %d bytes; stderr is not bounded", len(message))
	}
	if !strings.Contains(message, "the real cause") {
		t.Fatalf("exit error dropped the tail, which is the whole diagnostic: %.200q", message)
	}
	if strings.Contains(message, "noise line 0\n") {
		t.Fatal("exit error still carries the earliest stderr; the ring is not discarding the head")
	}
}

func TestLinesProcessDoesNotInheritCredentialEnvironment(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "lines-process-canary")
	t.Setenv("THIRD_PARTY_API_TOKEN", "unrelated-secret-canary")
	process, err := StartLinesProcess(context.Background(), "sh", []string{"-c", "printf '%s|%s\\n' \"$OPENROUTER_API_KEY\" \"$THIRD_PARTY_API_TOKEN\""})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Close() }()

	line, err := process.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(line)) != "|" {
		t.Fatalf("provider child inherited a credential environment variable: %q", line)
	}
}

func TestLinesProcessWithOptionsUsesExplicitWorkspace(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("OPENROUTER_API_KEY", "persistent-process-canary")
	t.Setenv("THIRD_PARTY_API_TOKEN", "persistent-unrelated-secret-canary")
	process, err := StartLinesProcessWithOptions(context.Background(), "sh", []string{"-c", "pwd; printf '%s|%s\\n' \"$OPENROUTER_API_KEY\" \"$THIRD_PARTY_API_TOKEN\""}, ProcessOptions{Dir: workspace})
	if err != nil {
		t.Fatal(err)
	}

	var lines []string
	for {
		line, nextErr := process.Next(context.Background())
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			_ = process.Close()
			t.Fatal(nextErr)
		}
		lines = append(lines, string(line))
	}
	if err := process.Close(); err != nil {
		t.Fatal(err)
	}

	wantWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[0] != wantWorkspace {
		t.Fatalf("persistent process lines = %q, want workspace %q and a second environment line", lines, wantWorkspace)
	}
	if lines[1] != "|" {
		t.Fatalf("persistent process inherited a credential: %q", lines[1])
	}
}

func TestLinesProcessWithOptionsRejectsInvalidWorkspace(t *testing.T) {
	for _, workspace := range []string{"relative/workspace", "/path/that/does/not/exist"} {
		if _, err := StartLinesProcessWithOptions(context.Background(), "sh", []string{"-c", "exit 0"}, ProcessOptions{Dir: workspace}); err == nil {
			t.Fatalf("workspace %q was accepted", workspace)
		}
	}
}

func TestRunLinesWithOptionsUsesExplicitWorkspace(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("OPENROUTER_API_KEY", "one-shot-process-canary")
	t.Setenv("THIRD_PARTY_API_TOKEN", "one-shot-unrelated-secret-canary")
	var lines []string
	err := RunLinesWithOptions(context.Background(), "sh", []string{"-c", "pwd; printf '%s|%s\\n' \"$OPENROUTER_API_KEY\" \"$THIRD_PARTY_API_TOKEN\""}, nil, func(line []byte) error {
		lines = append(lines, string(line))
		return nil
	}, ProcessOptions{Dir: workspace})
	if err != nil {
		t.Fatal(err)
	}

	wantWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[0] != wantWorkspace {
		t.Fatalf("one-shot process lines = %q, want workspace %q and a second environment line", lines, wantWorkspace)
	}
	if lines[1] != "|" {
		t.Fatalf("one-shot process inherited a credential: %q", lines[1])
	}
}

func TestRunLinesWithOptionsRejectsInvalidWorkspace(t *testing.T) {
	for _, workspace := range []string{"relative/workspace", "/path/that/does/not/exist"} {
		err := RunLinesWithOptions(context.Background(), "sh", []string{"-c", "exit 0"}, nil, func([]byte) error { return nil }, ProcessOptions{Dir: workspace})
		if err == nil {
			t.Fatalf("workspace %q was accepted", workspace)
		}
	}
}

// Cancelling a provider session must take the vendor's own children with it.
// Running the vendor's tool loop is the entire premise of this backend, so a
// `bash`, an `npm test`, a language server it started are all grandchildren of
// kolk. exec.CommandContext's default cancel signals only the direct child, so
// without a process group those survive the session that started them — the
// same defect TestTimeoutKillsTheWholeProcessGroup exists to prevent on the
// Run path, on the path that is actually long-lived.
func TestLinesProcessCancelKillsTheWholeProcessGroup(t *testing.T) {
	// The grandchild here is `sleep 30 &`, and POSIX makes a background job in
	// a non-interactive shell ignore SIGINT — so this one genuinely survives
	// rung 1 and dies on rung 2. Shortening the graces keeps that a test about
	// the group rather than about the five-second wait.
	shortLadder(t)
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "grandchild.pid")
	// Start a grandchild in the background, record its pid, then block. It
	// outlives the session unless the whole group is signalled.
	script := fmt.Sprintf("sleep 30 & echo $! > %s; wait", pidFile)

	ctx, cancel := context.WithCancel(context.Background())
	process, err := StartLinesProcess(ctx, "sh", []string{"-c", script})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	defer func() { _ = process.Close() }()

	pid := readPID(t, pidFile)
	if pid <= 0 {
		cancel()
		t.Skip("the grandchild never recorded its pid; nothing to assert")
	}
	cancel()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !alive(pid) {
			return // the group died with its leader
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("grandchild %d survived the cancelled session; the process group is not being signalled", pid)
}

// Amendment A6's case, and it is a diagnosis bug rather than a plumbing one.
// A child that dies on a bad flag has already said why, in stderr. But the
// prompt is large, the pipe buffer is 64 KiB, and a synchronous write to a dead
// child blocks and then fails with EPIPE — so the turn reports "broken pipe"
// and throws away the child's own explanation. The user is told about a pipe
// when they should be told about the flag.
func TestALargePromptToADeadChildReportsTheChildsOwnReason(t *testing.T) {
	process, err := StartLinesProcess(context.Background(), "sh",
		[]string{"-c", `echo "claude: unknown flag --nope" >&2; exit 2`})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Close() }()

	// Comfortably past the 64 KiB pipe buffer, which is what makes the write
	// block rather than fail fast.
	huge := bytes.Repeat([]byte("x"), 256*1024)
	if sendErr := process.Send(huge); sendErr != nil {
		t.Fatalf("Send reported %v; a prompt the child never read is not the caller's error", sendErr)
	}

	_, err = process.Next(context.Background())
	if err == nil {
		t.Fatal("a child that exits 2 must report its failure")
	}
	if !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("error = %v, want the child's own reason rather than a pipe symptom", err)
	}
}

func TestLinesProcessReusesOneChildForMultipleLines(t *testing.T) {
	process, err := StartLinesProcess(context.Background(), "cat", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Close() }()
	for _, want := range []string{"first", "second"} {
		if err := process.Send([]byte(want)); err != nil {
			t.Fatal(err)
		}
		got, err := process.Next(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Fatalf("line = %q, want %q", got, want)
		}
	}
}

func TestLinesProcessAcceptsALargeProviderLine(t *testing.T) {
	process, err := StartLinesProcess(context.Background(), "cat", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Close() }()

	want := bytes.Repeat([]byte("x"), 12*1024*1024)
	if err := process.Send(want); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := process.Next(ctx)
	if err != nil {
		t.Fatalf("reading the large provider line: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("line length/content = %d/%q, want %d bytes of x", len(got), got[:min(len(got), 20)], len(want))
	}
}

// withinTimeout fails instead of hanging the suite when a call blocks forever.
func withinTimeout(t *testing.T, what string, call func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		call()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s blocked instead of returning", what)
	}
}

func TestLinesProcessReportsExitRepeatablyWithoutBlocking(t *testing.T) {
	process, err := StartLinesProcess(context.Background(), "sh", []string{"-c", "exit 0"})
	if err != nil {
		t.Fatal(err)
	}

	var firstErr, secondErr, closeErr error
	withinTimeout(t, "the first Next after the child exits", func() {
		_, firstErr = process.Next(context.Background())
	})
	withinTimeout(t, "a second Next after the child exits", func() {
		_, secondErr = process.Next(context.Background())
	})
	withinTimeout(t, "Close after the child exits", func() {
		closeErr = process.Close()
	})

	// A clean end of stream must still be an error value. Returning a nil line
	// with a nil error tells a reader loop "keep reading", which spins forever.
	if !errors.Is(firstErr, io.EOF) {
		t.Fatalf("end of a cleanly exited stream = %v, want io.EOF", firstErr)
	}
	if !errors.Is(secondErr, io.EOF) {
		t.Fatalf("repeated end of stream = %v, want the same io.EOF", secondErr)
	}
	if closeErr != nil {
		t.Fatalf("Close after exit = %v", closeErr)
	}
}

func TestLinesProcessCloseIsRepeatableAfterAFailedChild(t *testing.T) {
	process, err := StartLinesProcess(context.Background(), "sh", []string{"-c", "exit 3"})
	if err != nil {
		t.Fatal(err)
	}

	var first, second error
	withinTimeout(t, "the first Close", func() { first = process.Close() })
	withinTimeout(t, "a repeated Close", func() { second = process.Close() })
	if first == nil {
		t.Fatal("a child that exits 3 must be reported as a failure")
	}
	if second == nil || second.Error() != first.Error() {
		t.Fatalf("repeated Close = %v, want the same failure as %v", second, first)
	}
}

func TestLinesProcessCloseUnblocksUnreadOutput(t *testing.T) {
	// The provider writes a response and then stays alive. With an unbuffered
	// lines channel and no Next caller, the reader parks trying to deliver the
	// response; Close must cancel that delivery before waiting for the child.
	process, err := StartLinesProcess(context.Background(), "sh", []string{"-c", "printf 'unread\\n'; sleep 30"})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-time.After(100 * time.Millisecond):
	case <-process.exited:
		t.Fatal("provider exited before the shutdown test could exercise unread output")
	}

	done := make(chan error, 1)
	go func() { done <- process.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close returned %v", err)
		}
	case <-time.After(2 * time.Second):
		_ = killChild(process.cmd)
		t.Fatal("Close blocked on unread provider output")
	}
}
