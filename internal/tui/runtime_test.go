package tui

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"
)

func TestRuntimeStreamsWhileRetainingTypeAheadAndCancelsOneTurn(t *testing.T) {
	input := newGatedInput([]byte("first request\r"), []byte("next draft\x03\x04"))
	var output bytes.Buffer
	started := make(chan struct{})
	var runtime *Runtime
	runtime = NewRuntime(RuntimeOptions{
		Input: input, Output: &output, Width: func() int { return 60 }, Height: func() int { return 12 },
		Status: Status{Model: "model", Mode: "code", Lifecycle: "ready"},
		Turn: func(ctx context.Context, prompt string) error {
			if prompt != "first request" {
				t.Errorf("turn prompt = %q", prompt)
			}
			close(started)
			if _, err := runtime.Write([]byte("assistant streaming")); err != nil {
				return err
			}
			<-ctx.Done()
			return ctx.Err()
		},
	})
	input.releaseWhen(started)

	if err := runtime.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := runtime.Snapshot()
	if got.Draft != "" || got.Transcript != "assistant streaming" {
		t.Fatalf("runtime mixed draft/output: %#v", got)
	}
	if got.Status.Lifecycle != "interrupted" {
		t.Fatalf("lifecycle = %q, want interrupted", got.Status.Lifecycle)
	}
	for _, sequence := range []string{"\x1b[?2004h", "\x1b[?25l", "\x1b[?25h", "\x1b[?2004l"} {
		if bytes.Count(output.Bytes(), []byte(sequence)) != 1 {
			t.Fatalf("terminal sequence %q count != 1 in %q", sequence, output.String())
		}
	}
}

func TestRuntimeSuccessfulTurnFinishesReadyBeforeEOF(t *testing.T) {
	finished := make(chan struct{})
	input := newGatedInput([]byte("successful request\r"), nil)
	input.releaseWhen(finished)
	runtime := NewRuntime(RuntimeOptions{
		Input: input, Output: io.Discard,
		Status: Status{Mode: "code", Lifecycle: "ready"},
		Turn: func(context.Context, string) error {
			close(finished)
			return nil
		},
	})
	if err := runtime.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := runtime.Snapshot().Status.Lifecycle; got != "ready" {
		t.Fatalf("successful turn lifecycle = %q, want ready", got)
	}
}

func TestRuntimeDoubleInterruptExitsWithoutEOF(t *testing.T) {
	input, writer := io.Pipe()
	defer writer.Close()
	runtime := NewRuntime(RuntimeOptions{Input: input, Output: io.Discard})
	done := make(chan error, 1)
	go func() { done <- runtime.Run(context.Background()) }()
	if _, err := writer.Write([]byte("discard me\x03\x03")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("double Ctrl+C did not close the runtime")
	}
}

func TestRuntimeApprovalBlocksTheTurnWithoutConsumingMainDraft(t *testing.T) {
	var output bytes.Buffer
	runtime := NewRuntime(RuntimeOptions{
		Input: bytes.NewReader(nil), Output: &output,
		Width: func() int { return 60 }, Height: func() int { return 12 },
		Status: Status{Mode: "code", Lifecycle: "thinking"},
	})
	runtime.Controller().HandleKey(Key{Kind: KeyText, Text: "next draft"})

	result := make(chan bool, 1)
	go func() {
		result <- runtime.Confirm(context.Background(), Approval{
			Action: "Run shell command", Detail: "go test ./...",
		})
	}()
	waitForApproval(t, runtime)
	runtime.HandleKey(Key{Kind: KeyText, Text: "y"})
	runtime.HandleKey(Key{Kind: KeyEnter})

	select {
	case allowed := <-result:
		if !allowed {
			t.Fatal("visible y approval was denied")
		}
	case <-time.After(time.Second):
		t.Fatal("approval did not unblock")
	}
	if got := runtime.Snapshot().Draft; got != "next draft" {
		t.Fatalf("approval consumed main draft: %q", got)
	}
}

func TestRuntimeToolWorkUsesOnlyTheEphemeralActivityRegion(t *testing.T) {
	runtime := NewRuntime(RuntimeOptions{Output: io.Discard, Status: Status{Mode: "code", Lifecycle: "thinking"}})
	stop := runtime.StartWork(context.Background(), "Reading file — PLAN.md")
	got := runtime.Snapshot()
	if got.Activity != "🐙 Reading file — PLAN.md…" || got.Transcript != "" || got.Status.Lifecycle != "working" {
		t.Fatalf("tool activity regions = %#v", got)
	}
	stop()
	if got := runtime.Snapshot(); got.Activity != "" || got.Transcript != "" {
		t.Fatalf("stopped tool activity leaked into transcript: %#v", got)
	}
}

func TestRuntimeTurnCanExitWithoutWaitingForAnotherKey(t *testing.T) {
	input, writer := io.Pipe()
	defer writer.Close()
	runtime := NewRuntime(RuntimeOptions{
		Input: input, Output: io.Discard,
		Turn: func(context.Context, string) error { return ErrExit },
	})
	done := make(chan error, 1)
	go func() { done <- runtime.Run(context.Background()) }()
	if _, err := writer.Write([]byte("/exit\r")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime waited for another key after /exit")
	}
}

type gatedInput struct {
	mu      sync.Mutex
	chunks  [][]byte
	release <-chan struct{}
	index   int
}

func newGatedInput(chunks ...[]byte) *gatedInput { return &gatedInput{chunks: chunks} }

func (r *gatedInput) releaseWhen(release <-chan struct{}) { r.release = release }

func (r *gatedInput) Read(p []byte) (int, error) {
	r.mu.Lock()
	index := r.index
	r.index++
	r.mu.Unlock()
	if index >= len(r.chunks) {
		return 0, io.EOF
	}
	if index > 0 && r.release != nil {
		<-r.release
	}
	return copy(p, r.chunks[index]), nil
}

func waitForApproval(t *testing.T, runtime *Runtime) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for runtime.Approval() == nil {
		if time.Now().After(deadline) {
			t.Fatal("approval did not become visible")
		}
		time.Sleep(time.Millisecond)
	}
}
