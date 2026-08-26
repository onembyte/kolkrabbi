package agentcli

import (
	"context"
	"io"
	"reflect"
	"testing"
)

func TestRunClaudeStreamsTranslatedEventsAndPromptOnStdin(t *testing.T) {
	invocation, err := BuildClaudeInvocation("opus", "high", "say hello")
	if err != nil {
		t.Fatal(err)
	}
	var got []Event
	err = runClaude(context.Background(), invocation, func(_ context.Context, executable string, args []string, stdin io.Reader, onLine func([]byte) error) error {
		if executable != "claude" || len(args) == 0 {
			t.Fatalf("runner command = %q %v", executable, args)
		}
		b, _ := io.ReadAll(stdin)
		if string(b) != "say hello\n" {
			t.Fatalf("stdin = %q", b)
		}
		for _, line := range []string{
			`{"type":"assistant","message":{"model":"opus","content":[{"type":"text","text":"hello"}]}}`,
			`{"type":"result","result":"ok","subtype":"success"}`,
		} {
			if err := onLine([]byte(line)); err != nil {
				return err
			}
		}
		return nil
	}, func(event Event) { got = append(got, event) })
	if err != nil {
		t.Fatal(err)
	}
	want := []Event{
		{Kind: EventMessageDelta, Model: "opus", Text: "hello"},
		{Kind: EventMessageCompleted, Text: "ok"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %+v, want %+v", got, want)
	}
}
