package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/session"
	"github.com/onembyte/kolkrabbi/internal/tui"
)

func TestBareEffortPickerSelectsCurrentAndDispatchesExistingCommand(t *testing.T) {
	a, out, _ := newTestApp(t, "")
	ag := engine.New(engine.Options{Model: "mock/model", Mode: engine.ModeCode, Effort: "medium",
		Sess: session.New(t.TempDir(), "mock/model")})
	command, shown := tuiEffortPickerCommand(context.Background(), func(_ context.Context, q tui.Question) (string, bool) {
		if q.Title != "effort" || q.InitialIndex != 1 || len(q.Options) != 5 ||
			!strings.Contains(q.Options[1], "current") || !strings.Contains(q.Prompt, "Esc") {
			t.Fatalf("effort overlay = %+v", q)
		}
		return q.Options[2], true
	}, ag, " /effort ")
	if !shown || command != "/effort high" || ag.Effort != "medium" {
		t.Fatalf("picker = %q, %v, effort %q", command, shown, ag.Effort)
	}
	a.slash(context.Background(), ag, command)
	if ag.Effort != "high" || ag.Sess.SessionEffort() != "high" || !strings.Contains(out.String(), "effort: high") {
		t.Fatalf("existing effort command not applied/persisted: %q, %q, %q", ag.Effort, ag.Sess.SessionEffort(), out.String())
	}
}

func TestEffortPickerCancelAndExplicitArgumentsDoNotChangeTheSession(t *testing.T) {
	ag := engine.New(engine.Options{Model: "mock/model", Effort: "xhigh"})
	before := ag.Effort
	command, shown := tuiEffortPickerCommand(context.Background(), func(_ context.Context, q tui.Question) (string, bool) {
		if q.InitialIndex != 3 {
			t.Fatalf("xhigh did not select canonical max: %+v", q)
		}
		return "", false
	}, ag, "/effort")
	if !shown || command != "" || ag.Effort != before {
		t.Fatalf("dismissal fell through or mutated effort: %q, %v, %q", command, shown, ag.Effort)
	}
	for _, prompt := range []string{"/effort high", "/effort 4", "/effort nonsense", "/model"} {
		command, shown := tuiEffortPickerCommand(context.Background(), func(context.Context, tui.Question) (string, bool) {
			t.Fatal("explicit command opened an overlay")
			return "", false
		}, ag, prompt)
		if shown || command != "" {
			t.Fatalf("%q claimed by picker", prompt)
		}
	}
}

func TestPlainEffortCommandRetainsItsStatusOutput(t *testing.T) {
	a, out, _ := newTestApp(t, "")
	ag := engine.New(engine.Options{Model: "mock/model", Effort: "medium"})
	a.slash(context.Background(), ag, "/effort")
	if got := out.String(); !strings.Contains(got, "effort: medium (low|medium|high|max|ultra)") {
		t.Fatalf("plain output changed: %q", got)
	}
}

// Readiness comes from actual rendered frames, not a delay after typing. This
// exercises the real TUI dispatch: helper-only tests cannot detect an overlay
// that was built but never connected to the interactive command.
type effortFrameWriter struct {
	mu sync.Mutex
	bytes.Buffer
	opened, returned     chan struct{}
	openOnce, returnOnce sync.Once
	sawPicker            bool
}

func (w *effortFrameWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.Buffer.Write(p)
	frame := string(p)
	if strings.Contains(frame, "Choose Kolk effort") {
		w.sawPicker = true
		w.openOnce.Do(func() { close(w.opened) })
	} else if w.sawPicker && strings.Contains(frame, "state ready") {
		w.returnOnce.Do(func() { close(w.returned) })
	}
	return n, err
}

func TestTUIBareEffortUsesThePickerRatherThanTheLegacyDump(t *testing.T) {
	for _, cancelPick := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%v", cancelPick), func(t *testing.T) {
			isolateHome(t)
			input, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			defer writer.Close()
			output := &effortFrameWriter{opened: make(chan struct{}), returned: make(chan struct{})}
			a := &app{stdout: output, stderr: output, terminalInput: input, terminalOutput: os.Stdout,
				enterRaw:     func(*os.File) (func() error, error) { return func() error { return nil }, nil },
				terminalSize: func(*os.File) (int, int) { return 100, 24 },
			}
			ag := engine.New(engine.Options{Model: "mock/model", Mode: engine.ModeCode, Effort: "medium",
				Sess: session.New(t.TempDir(), "mock/model"), Out: output})
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			writeDone := make(chan error, 1)
			go func() {
				if _, err := writer.Write([]byte("/effort\r")); err != nil {
					writeDone <- err
					return
				}
				select {
				case <-output.opened:
				case <-ctx.Done():
					writeDone <- fmt.Errorf("bare /effort never rendered its picker")
					return
				}
				keys := "\x1b[B\r" // medium → high, Enter
				if cancelPick {
					keys = "\x1b"
				}
				if _, err := writer.Write([]byte(keys)); err != nil {
					writeDone <- err
					return
				}
				select {
				case <-output.returned:
				case <-ctx.Done():
					writeDone <- fmt.Errorf("effort picker did not return to ready")
					return
				}
				_, err := writer.Write([]byte("/exit\r"))
				writeDone <- err
			}()
			if err := a.tuiRepl(ctx, ag); err != nil {
				t.Fatal(err)
			}
			if err := <-writeDone; err != nil {
				t.Fatal(err)
			}
			want := "high"
			if cancelPick {
				want = "medium"
			}
			if ag.Effort != want || strings.Contains(output.String(), "(low|medium|high|max|ultra)") {
				t.Fatalf("picker fell through or changed effort incorrectly: %q, output %q", ag.Effort, output.String())
			}
		})
	}
}
