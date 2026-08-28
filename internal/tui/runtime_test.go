package tui

import (
	"bytes"
	"context"
	"io"
	"strings"
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
	// The request is echoed into the transcript ahead of the reply, a cancelled
	// turn commits its marker where the output it stopped is, and the type-ahead
	// draft survives the interrupt that ended the turn.
	if got.Draft != "next draft" || got.Transcript != "❯ first request\nassistant streaming"+interruptedNotice {
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
	// One keypress answers; there is no Enter for the approval to consume, and
	// the main draft must survive both the overlay and the decision.
	runtime.HandleKey(Key{Kind: KeyText, Text: "y"})

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
	clock := newFakeSpinnerClock()
	runtime.spinClock = clock
	stop := runtime.StartWork(context.Background(), "Reading file — PLAN.md")
	got := runtime.Snapshot()
	if got.Activity != activityLine(0, "working") || got.Transcript != "" || got.Status.Lifecycle != "working" {
		t.Fatalf("tool activity regions = %#v", got)
	}
	// A tool's own description is too specific for the status row, and
	// "thinking" would claim a model call that is not happening.
	if strings.Contains(got.Activity, "thinking") || strings.Contains(got.Activity, "Reading file") {
		t.Fatalf("activity leaked a label it should not carry: %q", got.Activity)
	}
	timer := nextSpinnerTimer(t, clock, spinnerInterval)
	timer.fire()
	waitForActivity(t, runtime, activityLine(1, "working"))
	stop()
	if got := runtime.Snapshot(); got.Activity != "" || got.Transcript != "" {
		t.Fatalf("stopped tool activity leaked into transcript: %#v", got)
	}
}

type fakeSpinnerTimer struct {
	delay time.Duration
	c     chan time.Time
}

func (t *fakeSpinnerTimer) C() <-chan time.Time { return t.c }
func (t *fakeSpinnerTimer) Stop()               {}
func (t *fakeSpinnerTimer) fire()               { t.c <- time.Time{} }

type fakeSpinnerClock struct{ created chan *fakeSpinnerTimer }

func newFakeSpinnerClock() *fakeSpinnerClock {
	return &fakeSpinnerClock{created: make(chan *fakeSpinnerTimer, 4)}
}

func (c *fakeSpinnerClock) NewTimer(delay time.Duration) spinnerTimer {
	timer := &fakeSpinnerTimer{delay: delay, c: make(chan time.Time, 1)}
	c.created <- timer
	return timer
}

func nextSpinnerTimer(t *testing.T, clock *fakeSpinnerClock, want time.Duration) *fakeSpinnerTimer {
	t.Helper()
	select {
	case timer := <-clock.created:
		if timer.delay != want {
			t.Fatalf("spinner delay = %v, want %v", timer.delay, want)
		}
		return timer
	case <-time.After(time.Second):
		t.Fatal("spinner did not request its next timer")
		return nil
	}
}

func waitForActivity(t *testing.T, runtime *Runtime, want string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for runtime.Snapshot().Activity != want {
		if time.Now().After(deadline) {
			t.Fatalf("activity = %q, want %q", runtime.Snapshot().Activity, want)
		}
		time.Sleep(time.Millisecond)
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

// writeRecounts how many repaints reached the output: every paint after the
// first has to move the cursor up before erasing, and that carriage return is
// the seam between frames.
func writeRepaints(output *bytes.Buffer) int { return bytes.Count(output.Bytes(), []byte("\r\x1b[")) }

func TestRuntimeCoalescesAStreamFloodIntoAFewRepaints(t *testing.T) {
	var output bytes.Buffer
	runtime := NewRuntime(RuntimeOptions{
		Input: bytes.NewReader([]byte("\x04")), Output: &output,
		Width: func() int { return 60 }, Height: func() int { return 12 },
	})
	done := make(chan error, 1)
	go func() { done <- runtime.Run(context.Background()) }()

	// Two hundred writes is far more than one pacing window holds. Unpaced this
	// paints two hundred frames; paced, the flood costs one repaint plus the
	// window-close and shutdown flushes.
	var writeErr error
	for range 200 {
		if _, err := runtime.Write([]byte("token ")); err != nil {
			writeErr = err
			break
		}
	}
	if writeErr != nil {
		t.Fatal(writeErr)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := writeRepaints(&output); got > 10 {
		t.Fatalf("a 200-write flood produced %d repaints, want coalescing to a few", got)
	}
	// And the flood's last token must still be visible after the flush on exit.
	if !strings.Contains(output.String(), "token") {
		t.Fatalf("coalescing dropped the streamed content: %q", output.String())
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

// stagedInput releases each chunk on its own gate, so a test can hold the
// session open while asserting on a turn that is still running. gatedInput
// shares one gate across every chunk, which makes "type this, then quit later"
// impossible to express.
type stagedInput struct {
	mu     sync.Mutex
	chunks [][]byte
	gates  []<-chan struct{}
	index  int
}

func (s *stagedInput) Read(p []byte) (int, error) {
	s.mu.Lock()
	index := s.index
	s.index++
	s.mu.Unlock()
	if index >= len(s.chunks) {
		return 0, io.EOF
	}
	if gate := s.gates[index]; gate != nil {
		<-gate
	}
	return copy(p, s.chunks[index]), nil
}

func TestRuntimeQueuesEnterDuringATurnAndSendsItWhenTheTurnFinishes(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	bothSeen := make(chan struct{})

	var mu sync.Mutex
	var prompts []string

	input := &stagedInput{
		chunks: [][]byte{[]byte("one\r"), []byte("two\r"), []byte("\x04")},
		// "two" is typed once the first turn is running; Ctrl+D waits until the
		// queued turn has been observed, so shutdown never races the queue.
		gates: []<-chan struct{}{nil, firstStarted, bothSeen},
	}

	runtime := NewRuntime(RuntimeOptions{
		Input: input, Output: io.Discard,
		Status: Status{Mode: "code", Lifecycle: "ready"},
		Turn: func(_ context.Context, prompt string) error {
			mu.Lock()
			prompts = append(prompts, prompt)
			count := len(prompts)
			mu.Unlock()
			if count == 1 {
				close(firstStarted)
				<-releaseFirst
			}
			return nil
		},
	})

	done := make(chan error, 1)
	go func() { done <- runtime.Run(context.Background()) }()

	<-firstStarted
	// Enter during the turn must queue the draft, not drop it.
	waitFor(t, 2*time.Second, "the draft to be queued", func() bool {
		runtime.mu.Lock()
		defer runtime.mu.Unlock()
		return runtime.controller.Queued() == "two"
	})
	close(releaseFirst)

	// The queue drains on its own, with no further keypress.
	waitFor(t, 3*time.Second, "the queued request to be sent", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(prompts) == 2
	})
	close(bothSeen)

	if err := <-done; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(prompts) != 2 || prompts[0] != "one" || prompts[1] != "two" {
		t.Fatalf("prompts = %#v, want [one two] in order", prompts)
	}
}

// waitFor polls until condition holds, so a test never depends on a sleep
// being long enough on a loaded machine.
func waitFor(t *testing.T, limit time.Duration, what string, condition func() bool) {
	t.Helper()
	deadline := time.After(limit)
	for {
		if condition() {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %s", what)
		case <-time.After(5 * time.Millisecond):
		}
	}
}
