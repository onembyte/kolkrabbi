package cli

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/session"
)

// replFixture builds a REPL over a scripted provider: no network, no key.
func replFixture(t *testing.T, stdin string, steps ...enginetest.Step) (*app, *engine.Agent, *bytes.Buffer) {
	t.Helper()
	srv := enginetest.New(steps...)
	t.Cleanup(srv.Close)

	client := provider.NewClient("test-key")
	client.BaseURL = srv.URL

	var out bytes.Buffer
	a := &app{stdout: &out, stderr: &out, in: bufio.NewReader(strings.NewReader(stdin))}
	ag := engine.New(engine.Options{
		Client: client,
		Model:  "mock/model",
		Sess:   session.New(t.TempDir(), "mock/model"),
		Yolo:   true,
		Out:    io.Discard,
	})
	return a, ag, &out
}

// A piped script whose last line has no trailing newline used to be dropped:
// bufio.ReadString returns the line AND io.EOF together, and the loop returned
// on any error.
func TestReplRunsAFinalLineWithNoTrailingNewline(t *testing.T) {
	a, ag, out := replFixture(t, "/session") // deliberately no \n
	if err := a.repl(context.Background(), ag); err != nil {
		t.Fatalf("repl returned %v", err)
	}
	if !strings.Contains(out.String(), ag.Sess.ID) {
		t.Errorf("the last command was dropped; output was:\n%s", out.String())
	}
}

func TestReplExitsOnEOF(t *testing.T) {
	a, ag, _ := replFixture(t, "")
	if err := a.repl(context.Background(), ag); err != nil {
		t.Fatalf("empty input should be a clean exit, got %v", err)
	}
}

func TestReplExitsOnSlashExit(t *testing.T) {
	a, ag, out := replFixture(t, "/exit\n/session\n")
	if err := a.repl(context.Background(), ag); err != nil {
		t.Fatalf("repl returned %v", err)
	}
	if strings.Contains(out.String(), "id:    ") {
		t.Errorf("/exit did not stop the loop; /session still ran:\n%s", out.String())
	}
}

func TestReplReportsARealReadError(t *testing.T) {
	var out bytes.Buffer
	a := &app{stdout: &out, stderr: &out, in: bufio.NewReader(errReader{})}
	_, ag, _ := replFixture(t, "")
	if err := a.repl(context.Background(), ag); err == nil {
		t.Error("a broken stdin must be an error, not a silent clean exit")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestReplRunsATurnAndSlashCommands(t *testing.T) {
	a, ag, out := replFixture(t, "hello there\n/mode chat\n/yolo\n",
		enginetest.Step{Text: "hi back", Cost: 0.001})
	if err := a.repl(context.Background(), ag); err != nil {
		t.Fatalf("repl returned %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "mode: chat") {
		t.Errorf("/mode chat did not take effect:\n%s", got)
	}
	if !strings.Contains(got, "yolo mode: false") {
		t.Errorf("/yolo did not toggle off:\n%s", got)
	}
	if len(ag.Sess.Messages) < 3 {
		t.Errorf("the turn did not reach the session: %d messages", len(ag.Sess.Messages))
	}
}

func TestSlashUnknownCommandDoesNotExit(t *testing.T) {
	a, ag, out := replFixture(t, "")
	if a.slash(ag, "/nonsense") {
		t.Error("an unknown slash command must not quit the REPL")
	}
	if !strings.Contains(out.String(), "/help") {
		t.Errorf("an unknown slash command should point at /help, got %q", out.String())
	}
}
