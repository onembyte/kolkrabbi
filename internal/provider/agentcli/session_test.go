package agentcli

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

type fakeLineProcess struct {
	sent  []byte
	lines [][]byte
}

func (p *fakeLineProcess) Send(line []byte) error {
	p.sent = append([]byte(nil), line...)
	return nil
}
func (p *fakeLineProcess) Next(context.Context) ([]byte, error) {
	if len(p.lines) == 0 {
		return nil, io.EOF
	}
	line := p.lines[0]
	p.lines = p.lines[1:]
	return line, nil
}
func (p *fakeLineProcess) Close() error { return nil }

func TestClaudeSessionReusesProcessAndStreamsTurn(t *testing.T) {
	process := &fakeLineProcess{lines: [][]byte{
		[]byte(`{"type":"assistant","message":{"model":"opus","content":[{"type":"text","text":"hello"}]}}`),
		[]byte(`{"type":"result","result":"hello","subtype":"success"}`),
	}}
	session, err := newClaudeSession(context.Background(), "high", func(context.Context, string, []string) (lineProcess, error) {
		return process, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var tokens string
	message, _, err := session.Turn(context.Background(), []provider.Message{{Role: "user", Content: "hi"}}, "opus", func(token string) {
		tokens += token
	})
	if err != nil {
		t.Fatal(err)
	}
	if message.Content != "hello" || tokens != "hello" || len(process.sent) == 0 {
		t.Fatalf("message=%+v tokens=%q sent=%q", message, tokens, process.sent)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
}

// stallingLineProcess waits for cancellation at one chosen point in the
// stream, which is what a real provider looks like when the user interrupts a
// turn: the frames it had already queued are still in the pipe afterwards.
type stallingLineProcess struct {
	lines   [][]byte
	index   int
	stallAt int
	closed  bool
}

func (p *stallingLineProcess) Send([]byte) error { return nil }

func (p *stallingLineProcess) Next(ctx context.Context) ([]byte, error) {
	if p.index == p.stallAt {
		p.stallAt = -1
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if p.index >= len(p.lines) {
		return nil, io.EOF
	}
	line := p.lines[p.index]
	p.index++
	return line, nil
}

func (p *stallingLineProcess) Close() error {
	p.closed = true
	return nil
}

func claudeTurnFrames(text string) [][]byte {
	return [][]byte{
		[]byte(`{"type":"assistant","message":{"model":"opus","content":[{"type":"text","text":"` + text + `"}]}}`),
		[]byte(`{"type":"result","result":"` + text + `","subtype":"success"}`),
	}
}

func TestClaudeSessionDoesNotServeAnInterruptedTurnToTheNextOne(t *testing.T) {
	process := &stallingLineProcess{
		lines:   append(claudeTurnFrames("one"), claudeTurnFrames("two")...),
		stallAt: 1,
	}
	session, err := newClaudeSession(context.Background(), "high", func(context.Context, string, []string) (lineProcess, error) {
		return process, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	interrupted, cancel := context.WithCancel(context.Background())
	go cancel()
	if _, _, err := session.Turn(interrupted, []provider.Message{{Role: "user", Content: "first"}}, "opus", nil); err == nil {
		t.Fatal("an interrupted turn must report the interruption")
	}

	message, _, err := session.Turn(context.Background(), []provider.Message{{Role: "user", Content: "second"}}, "opus", nil)
	if err != nil {
		t.Fatalf("the turn after an interrupt failed: %v", err)
	}
	if message.Content != "two" {
		t.Fatalf("second turn answered %q — the interrupted turn's frames leaked into it", message.Content)
	}
}

func TestClaudeSessionReportsItselfUnusableWhenItCannotResynchronize(t *testing.T) {
	// Only the interrupted turn's opening frame is ever available, so the
	// completion frame that would resynchronize the stream never arrives.
	process := &stallingLineProcess{lines: claudeTurnFrames("one")[:1], stallAt: 1}
	session, err := newClaudeSession(context.Background(), "high", func(context.Context, string, []string) (lineProcess, error) {
		return process, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	interrupted, cancel := context.WithCancel(context.Background())
	go cancel()
	if _, _, err := session.Turn(interrupted, []provider.Message{{Role: "user", Content: "first"}}, "opus", nil); err == nil {
		t.Fatal("an interrupted turn must report the interruption")
	}

	if !session.Unusable() {
		t.Fatal("a session that cannot resynchronize must declare itself unusable")
	}
	_, _, err = session.Turn(context.Background(), []provider.Message{{Role: "user", Content: "second"}}, "opus", nil)
	if err == nil {
		t.Fatal("an unusable session must refuse the next turn instead of answering from a desynchronized stream")
	}
	if !strings.Contains(err.Error(), "interrupted") {
		t.Fatalf("refusal = %v, want it to name the interrupted turn", err)
	}
}

func TestClaudeSessionExplainsAProviderThatExitsMidTurn(t *testing.T) {
	session, err := newClaudeSession(context.Background(), "high", func(context.Context, string, []string) (lineProcess, error) {
		return &fakeLineProcess{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = session.Turn(context.Background(), []provider.Message{{Role: "user", Content: "hi"}}, "opus", nil)
	if err == nil {
		t.Fatal("a provider that exits mid-turn must be reported")
	}
	// "EOF" on its own tells the user nothing they can act on.
	if !strings.Contains(err.Error(), "claude exited before finishing") {
		t.Fatalf("error = %v, want an explanation of what happened", err)
	}
	if !strings.Contains(err.Error(), "claude") {
		t.Fatalf("error = %v, want the command the user can run to check", err)
	}
	if !errors.Is(err, io.EOF) {
		t.Fatalf("error = %v, want the underlying cause preserved", err)
	}
}
