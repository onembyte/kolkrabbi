package agentcli

import (
	"context"
	"io"
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
