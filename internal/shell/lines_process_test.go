//go:build !windows

package shell

import (
	"context"
	"testing"
	"time"
)

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

	if firstErr != nil {
		t.Fatalf("clean exit reported as an error: %v", firstErr)
	}
	if secondErr != nil {
		t.Fatalf("repeated exit report changed: %v", secondErr)
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
