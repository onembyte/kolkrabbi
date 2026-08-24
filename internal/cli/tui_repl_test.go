package cli

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/session"
)

func TestTUIReplOwnsAndRestoresOneInteractiveTerminal(t *testing.T) {
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	defer writer.Close()

	var output bytes.Buffer
	restored := 0
	a := &app{
		stdout: &output, stderr: &output,
		terminalInput: input, terminalOutput: os.Stdout,
		enterRaw: func(got *os.File) (func() error, error) {
			if got != input {
				t.Fatalf("raw input = %v, want test pipe", got)
			}
			return func() error { restored++; return nil }, nil
		},
		terminalSize: func(*os.File) (int, int) { return 72, 14 },
	}
	ag := engine.New(engine.Options{
		Model: "mock/model", Mode: engine.ModeCode, Effort: "standard",
		Sess: session.New(t.TempDir(), "mock/model"), Out: &output,
	})

	writeErr := make(chan error, 1)
	go func() {
		_, err := writer.Write([]byte("/exit\r"))
		writeErr <- err
	}()
	if err := a.tuiRepl(context.Background(), ag); err != nil {
		t.Fatal(err)
	}
	if err := <-writeErr; err != nil {
		t.Fatal(err)
	}
	if restored != 1 {
		t.Fatalf("terminal restore calls = %d, want one", restored)
	}
	got := output.String()
	for _, want := range []string{
		"\x1b[?2004h", "\x1b[?25l", "kolk-code", "mock/model", "↑ recalls history",
		"twice exits", "\x1b[?25h", "\x1b[?2004l",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("interactive output omitted %q: %q", want, got)
		}
	}
	if a.stdout != &output || a.stderr != &output {
		t.Fatal("TUI did not restore app output streams")
	}
}

func TestTUIEligibilityRequiresRealTerminalFilesAndRawMode(t *testing.T) {
	a := &app{canAnimate: func() bool { return true }}
	if a.canUseTUI() {
		t.Fatal("stream-only app selected the raw TUI")
	}
	a.terminalInput, a.terminalOutput = os.Stdin, os.Stdout
	a.enterRaw = func(*os.File) (func() error, error) { return func() error { return nil }, nil }
	a.terminalSize = func(*os.File) (int, int) { return 80, 24 }
	if !a.canUseTUI() {
		t.Fatal("fully interactive app did not select the TUI")
	}
	a.canAnimate = func() bool { return false }
	if a.canUseTUI() {
		t.Fatal("TERM-dumb or redirected app selected the TUI")
	}
}
