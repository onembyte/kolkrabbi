package cli

import (
	"bufio"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

type exitAfterWatch struct{ entered <-chan struct{} }

func (r exitAfterWatch) Read([]byte) (int, error) { <-r.entered; return 0, io.EOF }

func TestReplExitCancelsItsPauseWatcher(t *testing.T) {
	a, ag, _ := replFixture(t, "")
	defer ag.Close()
	entered, canceled := make(chan struct{}), make(chan struct{})
	ag.Sess.SetPaused(&continuity.Pause{Kind: "endpoint_capacity", Since: time.Now(), ResetAt: time.Now().Add(time.Hour), PendingTurn: "waiting task"})
	ag.ResumeWait = func(ctx context.Context, _ time.Duration) error {
		close(entered)
		<-ctx.Done()
		close(canceled)
		return ctx.Err()
	}
	a.in = bufio.NewReader(exitAfterWatch{entered})
	if err := a.repl(context.Background(), ag); err != nil {
		t.Fatal(err)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("REPL exited with a live resume watcher")
	}
	if got := ag.Sess.Paused(); got == nil || got.PendingTurn != "waiting task" {
		t.Fatalf("lost waiting task: %+v", got)
	}
}

type resumeExitBackend struct{ entered, canceled chan struct{} }

func (b resumeExitBackend) StreamChat(ctx context.Context, _ string, _ []provider.Message, _ []provider.Tool, _ func(string)) (provider.Message, provider.Meta, error) {
	close(b.entered)
	<-ctx.Done()
	close(b.canceled)
	return provider.Message{}, provider.Meta{}, ctx.Err()
}

func TestReplExitCancelsAndJoinsAnAutomaticallyDeliveredTurn(t *testing.T) {
	a, ag, _ := replFixture(t, "")
	defer ag.Close()
	b := resumeExitBackend{make(chan struct{}), make(chan struct{})}
	ag.SetSessionBackend(b)
	ag.Sess.SetPaused(&continuity.Pause{Kind: "endpoint_capacity", Since: time.Now(), PendingTurn: "waiting task"})
	ag.ResumeWait = func(context.Context, time.Duration) error { return nil }
	ag.ProbeLimit = func(context.Context, continuity.Pause) (bool, error) { return true, nil }
	a.in = bufio.NewReader(exitAfterWatch{b.entered})
	done := make(chan error, 1)
	go func() { done <- a.repl(context.Background(), ag) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		_ = ag.Close()
		<-done
		t.Fatal("REPL could not read EOF while an automatic turn was running")
	}
	select {
	case <-b.canceled:
	default:
		t.Fatal("REPL returned before its automatic turn was canceled")
	}
}

// linesAfter feeds its lines once entered is closed, then ends.
type linesAfter struct {
	entered <-chan struct{}
	lines   string
	sent    bool
}

func (r *linesAfter) Read(p []byte) (int, error) {
	if r.sent {
		return 0, io.EOF
	}
	<-r.entered
	r.sent = true
	return copy(p, r.lines), nil
}

// /new while an automatically delivered turn is running interrupts that turn,
// which belongs to the old session, and then starts the new one. It never
// waits on the turn lock the delivery holds: the session swap joins every
// delivery, so one still waiting for that lock would never be joined.
func TestReplNewInterruptsAnAutomaticallyDeliveredTurn(t *testing.T) {
	a, ag, out := replFixture(t, "")
	defer ag.Close()
	before := ag.Sess.SessionID()
	b := resumeExitBackend{make(chan struct{}), make(chan struct{})}
	ag.SetSessionBackend(b)
	ag.Sess.SetPaused(&continuity.Pause{Kind: "endpoint_capacity", Since: time.Now(), PendingTurn: "waiting task"})
	ag.ResumeWait = func(context.Context, time.Duration) error { return nil }
	ag.ProbeLimit = func(context.Context, continuity.Pause) (bool, error) { return true, nil }
	a.in = bufio.NewReader(&linesAfter{entered: b.entered, lines: "/new\n/exit\n"})
	done := make(chan error, 1)
	go func() { done <- a.repl(context.Background(), ag) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		_ = ag.Close()
		<-done
		t.Fatal("/new waited on the automatically delivered turn instead of interrupting it")
	}
	select {
	case <-b.canceled:
	default:
		t.Fatal("the delivered turn was not interrupted")
	}
	if ag.Sess.SessionID() == before || !strings.Contains(out.String(), "new session: ") {
		t.Fatalf("no new session; output:\n%s", out.String())
	}
}
