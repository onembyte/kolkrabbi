//go:build !windows

package shell

import (
	"context"
	"errors"
	"io"
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
