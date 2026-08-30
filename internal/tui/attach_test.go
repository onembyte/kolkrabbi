package tui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// The point of attaching: a child gets the session's keyboard without a second
// reader appearing on the terminal. The read goroutine stays the only reader
// and forwards, which is why the session survives instead of being handed away.
func TestAnAttachedChildReceivesTheKeyboard(t *testing.T) {
	input, keystrokes := io.Pipe()
	var screen bytes.Buffer
	r := NewRuntime(RuntimeOptions{Input: input, Output: &screen})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = r.Run(ctx) }()

	got := make(chan string, 1)
	go func() {
		_ = r.RunAttached(ctx, func(in io.Reader, _ io.Writer, _, _ int) error {
			buffer := make([]byte, 5)
			n, _ := io.ReadFull(in, buffer)
			got <- string(buffer[:n])
			return nil
		})
	}()

	// Give the attach a moment to take the keyboard, then type.
	deadline := time.Now().Add(2 * time.Second)
	for {
		r.mu.Lock()
		attached := r.attached != nil
		r.mu.Unlock()
		if attached || time.Now().After(deadline) {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	go func() { _, _ = keystrokes.Write([]byte("hello")) }()

	select {
	case typed := <-got:
		if typed != "hello" {
			t.Errorf("the child received %q, want \"hello\"", typed)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("keystrokes never reached the attached child")
	}
}

// While a child owns the terminal, its keystrokes must not also be interpreted
// as kolk's own keys — otherwise typing a password could trigger a command.
func TestKeystrokesAreNotDecodedWhileAttached(t *testing.T) {
	input, keystrokes := io.Pipe()
	var screen bytes.Buffer
	r := NewRuntime(RuntimeOptions{Input: input, Output: &screen})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = r.Run(ctx) }()

	release := make(chan struct{})
	go func() {
		_ = r.RunAttached(ctx, func(in io.Reader, _ io.Writer, _, _ int) error {
			buffer := make([]byte, 4)
			_, _ = io.ReadFull(in, buffer)
			<-release
			return nil
		})
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		r.mu.Lock()
		attached := r.attached != nil
		r.mu.Unlock()
		if attached || time.Now().After(deadline) {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	go func() { _, _ = keystrokes.Write([]byte("abcd")) }()
	time.Sleep(150 * time.Millisecond)

	if draft := r.Snapshot().Draft; draft != "" {
		t.Errorf("the composer captured %q while a child owned the keyboard", draft)
	}
	close(release)
}

// Two children on one terminal is the problem this path exists to solve, so it
// must not be reachable from inside it.
func TestASecondAttachIsRefused(t *testing.T) {
	r := NewRuntime(RuntimeOptions{})
	ctx := context.Background()
	inside := make(chan error, 1)
	release := make(chan struct{})
	go func() {
		_ = r.RunAttached(ctx, func(io.Reader, io.Writer, int, int) error {
			inside <- r.RunAttached(ctx, func(io.Reader, io.Writer, int, int) error { return nil })
			<-release
			return nil
		})
	}()
	select {
	case err := <-inside:
		if !errors.Is(err, ErrAlreadyAttached) {
			t.Errorf("second attach returned %v, want ErrAlreadyAttached", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the nested attach never returned")
	}
	close(release)
}

// H7: while attached, the read loop forwards raw bytes straight to the child
// and never calls HandleKey at all — so a question, an approval, or a
// picker opened during an attach would show on screen and then hang forever,
// since nothing routes a key to it until the attach itself ends. RunAttached
// must refuse the same way it already refuses a second attach.
func TestAnAttachIsRefusedWhileAModelPickerIsOpen(t *testing.T) {
	r := NewRuntime(RuntimeOptions{})
	pickDone := make(chan bool, 1)
	go func() {
		_, ok := r.AskModel(context.Background(), []ModelPickEntry{{ID: "vendor/model"}})
		pickDone <- ok
	}()
	waitForPickerOpen(t, r)

	attached := make(chan error, 1)
	go func() {
		attached <- r.RunAttached(context.Background(), func(io.Reader, io.Writer, int, int) error { return nil })
	}()
	select {
	case err := <-attached:
		if !errors.Is(err, ErrAlreadyAttached) {
			t.Errorf("attach while the /model picker was open returned %v, want ErrAlreadyAttached", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("RunAttached blocked instead of refusing while the /model picker was already open")
	}
	r.HandleKey(Key{Kind: KeyEscape})
	<-pickDone
}

// The session has to come back afterwards, not stay a blank screen.
func TestTheFrameComesBackAfterTheChildExits(t *testing.T) {
	var screen bytes.Buffer
	r := NewRuntime(RuntimeOptions{Output: &screen, Status: Status{Mode: "code", Lifecycle: "ready"}})
	if err := r.renderer.Start(); err != nil {
		t.Fatal(err)
	}
	if err := r.RunAttached(context.Background(), func(_ io.Reader, out io.Writer, _, _ int) error {
		_, _ = io.WriteString(out, "child output\n")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	written := screen.String()
	if !strings.Contains(written, "child output") {
		t.Error("the child's output never reached the screen")
	}
	if !strings.Contains(written, "kolkrabbi") {
		t.Error("the composer was not repainted after the child exited")
	}
	r.mu.Lock()
	still := r.attached
	r.mu.Unlock()
	if still != nil {
		t.Error("the terminal was not taken back from the child")
	}
}

// An error from the child is the caller's to report, not something to swallow.
func TestTheChildsErrorIsReturned(t *testing.T) {
	r := NewRuntime(RuntimeOptions{})
	want := io.ErrUnexpectedEOF
	if got := r.RunAttached(context.Background(), func(io.Reader, io.Writer, int, int) error {
		return want
	}); !errors.Is(got, want) {
		t.Errorf("RunAttached returned %v, want %v", got, want)
	}
	r.mu.Lock()
	still := r.attached
	r.mu.Unlock()
	if still != nil {
		t.Error("a failing child left the terminal attached")
	}
}
