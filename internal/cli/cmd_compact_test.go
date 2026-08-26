package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

func bulkySession() []provider.Message {
	messages := []provider.Message{{Role: "system", Content: "you are kolk"}}
	for _, name := range []string{"one", "two", "three", "four"} {
		messages = append(messages,
			provider.Message{Role: "user", Content: name},
			provider.Message{Role: "assistant", ToolCalls: []provider.ToolCall{{
				ID: "call-" + name, Type: "function",
				Function: provider.FunctionCall{Name: "bash", Arguments: `{"command":"go test"}`},
			}}},
			provider.Message{Role: "tool", ToolCallID: "call-" + name, Content: strings.Repeat("output ", 2000)},
			provider.Message{Role: "assistant", Content: "done " + name},
		)
	}
	return messages
}

func TestSlashCompactShrinksOnDemandAndCanBeUndone(t *testing.T) {
	a, ag, out := replFixture(t, "")
	ag.Sess.SetMessages(bulkySession())
	ag.ContextWindow = 20_000
	before := len(ag.Sess.GetMessages()[3].Content)

	if a.slash(context.Background(), ag, "/compact") {
		t.Fatal("/compact must not exit the session")
	}
	if got := out.String(); !strings.Contains(got, "compacted") || !strings.Contains(got, "/compact undo") {
		t.Fatalf("output = %q, want the result and how to reverse it", got)
	}
	if len(ag.Sess.GetMessages()[3].Content) >= before {
		t.Fatal("the session did not shrink")
	}

	if a.slash(context.Background(), ag, "/compact undo") {
		t.Fatal("/compact undo must not exit the session")
	}
	if len(ag.Sess.GetMessages()[3].Content) != before {
		t.Fatal("undo did not put the conversation back")
	}
	if !strings.Contains(out.String(), "restored") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestSlashCompactUndoWithNothingToUndoSaysSo(t *testing.T) {
	a, ag, out := replFixture(t, "")

	if a.slash(context.Background(), ag, "/compact undo") {
		t.Fatal("/compact undo must not exit the session")
	}
	if !strings.Contains(out.String(), "no compaction to undo") {
		t.Fatalf("output = %q, want an honest no-op", out.String())
	}
}

func TestSlashCompactOnAShortSessionChangesNothing(t *testing.T) {
	a, ag, out := replFixture(t, "")
	ag.ContextWindow = 200_000

	if a.slash(context.Background(), ag, "/compact") {
		t.Fatal("/compact must not exit the session")
	}
	if !strings.Contains(out.String(), "nothing to compact") {
		t.Fatalf("output = %q", out.String())
	}
}
