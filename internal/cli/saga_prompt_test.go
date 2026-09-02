package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
)

func TestReplInlineSagaUsesTheCurrentAgentAndRestoresOrdinaryPosture(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	var out bytes.Buffer
	a := &app{
		stdout: &out,
		stderr: &out,
		in:     bufio.NewReader(strings.NewReader("build the app /saga\n/exit\n")),
	}
	ag := engine.New(engine.Options{
		Model: "mock/model",
		Mode:  engine.ModeCode,
		Sess:  enginetest.NewFakeSession("saga-session", "mock/model"),
		Out:   io.Discard,
	})
	var wakes int
	a.sagaWake = func(_ context.Context, got *engine.Agent) error {
		wakes++
		if got != ag {
			t.Fatal("inline SAGA created a second agent instead of using the current one")
		}
		if got.Posture != engine.PostureSaga {
			t.Fatalf("wake posture = %q, want %q", got.Posture, engine.PostureSaga)
		}
		return nil
	}

	if err := a.repl(context.Background(), ag); err != nil {
		t.Fatalf("repl returned %v", err)
	}
	if wakes != 1 {
		t.Fatalf("SAGA wakes = %d, want one", wakes)
	}
	if ag.Posture != engine.Posture("") {
		t.Fatalf("posture after wake = %q, want ordinary posture", ag.Posture)
	}
	body, err := os.ReadFile(filepath.Join(root, "SAGA.md"))
	if err != nil {
		t.Fatalf("SAGA.md was not created: %v", err)
	}
	if !strings.Contains(string(body), "- **Goal**: build the app") {
		t.Fatalf("SAGA.md = %q, want marker removed from the goal", body)
	}
}

func TestInlineSagaRestoresPostureWhenTheWakeFails(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	ag := engine.New(engine.Options{
		Model: "mock/model",
		Mode:  engine.ModeCode,
		Sess:  enginetest.NewFakeSession("saga-error", "mock/model"),
		Out:   io.Discard,
	})
	wakeErr := errors.New("wake failed")
	a := &app{
		stdout: &bytes.Buffer{},
		stderr: &bytes.Buffer{},
		sagaWake: func(_ context.Context, got *engine.Agent) error {
			if got.Posture != engine.PostureSaga {
				t.Fatalf("wake posture = %q, want %q", got.Posture, engine.PostureSaga)
			}
			return wakeErr
		},
	}

	err := a.runInteractivePrompt(context.Background(), ag, "build it /saga")
	if !errors.Is(err, wakeErr) {
		t.Fatalf("inline SAGA error = %v, want wake error", err)
	}
	if ag.Posture != engine.Posture("") {
		t.Fatalf("posture after failed wake = %q, want ordinary posture", ag.Posture)
	}
}

func TestInlineSagaRefusesOutsideARepositoryBeforeWritingArtifact(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	ag := engine.New(engine.Options{
		Model: "mock/model",
		Mode:  engine.ModeCode,
		Sess:  enginetest.NewFakeSession("saga-no-repo", "mock/model"),
		Out:   io.Discard,
	})
	a := &app{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	if err := a.runInteractivePrompt(context.Background(), ag, "build it /saga"); err == nil {
		t.Fatal("inline SAGA succeeded outside a repository")
	} else if !strings.Contains(err.Error(), "not inside a git repository") {
		t.Fatalf("non-repository error = %v, want actionable repository error", err)
	}
	if _, err := os.Stat(filepath.Join(root, "SAGA.md")); !os.IsNotExist(err) {
		t.Fatalf("non-repository invocation created SAGA.md: %v", err)
	}
}

func TestTUIInlineSagaUsesTheSharedCurrentSessionBoundary(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	a, ag, _ := replFixture(t, "", enginetest.Step{Text: "ordinary path must not run"})
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	defer writer.Close()
	a.terminalInput = input
	a.terminalOutput = os.Stdout
	a.enterRaw = func(*os.File) (func() error, error) { return func() error { return nil }, nil }
	a.terminalSize = func(*os.File) (int, int) { return 80, 20 }

	var wakes int
	a.sagaWake = func(_ context.Context, got *engine.Agent) error {
		wakes++
		if got != ag {
			t.Fatal("TUI inline SAGA created a second agent")
		}
		if got.Posture != engine.PostureSaga {
			t.Fatalf("TUI wake posture = %q, want %q", got.Posture, engine.PostureSaga)
		}
		return nil
	}

	writeErr := make(chan error, 1)
	go func() {
		_, writeErrValue := writer.Write([]byte("/saga build it\r/exit\r"))
		writeErr <- writeErrValue
	}()
	if err := a.tuiRepl(context.Background(), ag); err != nil {
		t.Fatalf("tuiRepl returned %v", err)
	}
	if err := <-writeErr; err != nil {
		t.Fatal(err)
	}
	if wakes != 1 {
		t.Fatalf("TUI SAGA wakes = %d, want one", wakes)
	}
	if ag.Posture != engine.Posture("") {
		t.Fatalf("TUI posture after wake = %q, want ordinary posture", ag.Posture)
	}
}

func TestTUIInlineSagaEscapeCancelsTheWakeAndRestoresPosture(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	a, ag, _ := replFixture(t, "")
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	defer writer.Close()
	a.terminalInput = input
	a.terminalOutput = os.Stdout
	a.enterRaw = func(*os.File) (func() error, error) { return func() error { return nil }, nil }
	a.terminalSize = func(*os.File) (int, int) { return 80, 20 }

	started := make(chan struct{})
	canceled := make(chan struct{})
	a.sagaWake = func(ctx context.Context, got *engine.Agent) error {
		if got.Posture != engine.PostureSaga {
			t.Fatalf("TUI wake posture = %q, want %q", got.Posture, engine.PostureSaga)
		}
		close(started)
		<-ctx.Done()
		close(canceled)
		return ctx.Err()
	}

	runDone := make(chan error, 1)
	go func() { runDone <- a.tuiRepl(context.Background(), ag) }()
	if _, err := writer.Write([]byte("/saga build it\r")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("TUI did not start the inline SAGA wake")
	}
	if _, err := writer.Write([]byte{0x1b}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("Escape did not cancel the active SAGA wake")
	}
	// Escape intentionally drops queued input. Close the input after the wake
	// has observed cancellation so Runtime exits through its EOF/join path
	// instead of racing a discarded `/exit` behind the interrupted turn.
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("tuiRepl returned %v after canceled wake and EOF", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("TUI did not exit after the canceled wake")
	}
	if ag.Posture != engine.Posture("") {
		t.Fatalf("TUI posture after cancellation = %q, want ordinary posture", ag.Posture)
	}
}
