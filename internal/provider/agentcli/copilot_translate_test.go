package agentcli

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

func copilotFixtureEvents(t *testing.T, name string) []Event {
	t.Helper()
	f, err := os.Open("testdata/copilot/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var events []Event
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		got, err := TranslateCopilot(scanner.Bytes())
		if err != nil {
			t.Fatalf("translate %q: %v", scanner.Text(), err)
		}
		events = append(events, got...)
	}
	return events
}

// V34.4c.2b, from a live run on 2026-09-06 (Copilot CLI 1.0.82, Free plan):
// `--output-format json` is a JSONL event stream. The reply arrives as
// assistant.message_delta then assistant.message; usage per model call sits
// in model.model_call_success; the terminal `result` carries the session id
// and exit code; the model the vendor chose is in the events, not the argv.
func TestTranslateCopilotReadsTheReplyUsageAndSessionFromTheStream(t *testing.T) {
	events := copilotFixtureEvents(t, "pong.jsonl")
	message, meta, err := Collect(events, 0)
	if err != nil {
		t.Fatal(err)
	}
	if message.Role != "assistant" || message.Content != "pong" {
		t.Fatalf("message = %+v, want the streamed reply", message)
	}
	if meta.Model != "gpt-5.6-luna" {
		t.Fatalf("model = %q, want the one the vendor chose", meta.Model)
	}
	if meta.PromptTokens != 14788 || meta.CompletionTokens != 5 || meta.CacheCreationTokens != 14785 {
		t.Fatalf("usage = %+v, want the call's own counts", meta)
	}
	session := ""
	for _, event := range events {
		if event.SessionID != "" {
			session = event.SessionID
		}
	}
	if session != "082b7ee5-7873-4b24-bea0-80a6235933d4" {
		t.Fatalf("session id = %q, want the result's", session)
	}
}

// A tool run: the tool's start and result are events with the tool's name,
// input and output, and the final assistant.message is the reply — the
// earlier one that only carried the tool request is not.
func TestTranslateCopilotCarriesToolCallsAndTheFinalReply(t *testing.T) {
	events := copilotFixtureEvents(t, "tool.jsonl")
	message, meta, err := Collect(events, 0)
	if err != nil {
		t.Fatal(err)
	}
	if message.Content != "done" {
		t.Fatalf("reply = %q, want the final message", message.Content)
	}
	if meta.ToolCalls != 1 {
		t.Fatalf("tool calls = %d, want the one apply_patch", meta.ToolCalls)
	}
	// The start names the tool and carries its input; the completion, tied
	// by call id, carries the result — the CLI repeats no name on it.
	var start, done Event
	for _, event := range events {
		if event.Kind != EventTool {
			continue
		}
		if event.ToolName == "apply_patch" {
			start = event
		} else if start.ToolCallID != "" && event.ToolCallID == start.ToolCallID {
			done = event
		}
	}
	if start.ToolName == "" || !strings.Contains(start.ToolInput, "Add File: hello.txt") {
		t.Fatalf("tool start = %+v, want the name and the patch input", start)
	}
	if !strings.Contains(done.ToolOutput, "Added 1 file") || done.ToolIsError {
		t.Fatalf("tool completion = %+v, want the result", done)
	}
}

// The gate, observed: without --allow-all-tools a non-interactive run cannot
// ask, the tool is denied, the model still says "done" and the exit code is 0.
// Kolk must not swallow that: the denial is a visible tool error, and the
// reply is not mistaken for success.
func TestTranslateCopilotSurfacesADeniedToolInsteadOfSwallowingIt(t *testing.T) {
	events := copilotFixtureEvents(t, "denied.jsonl")
	message, _, err := Collect(events, 0)
	if err != nil {
		t.Fatal(err)
	}
	if message.Content != "done" {
		t.Fatalf("reply = %q", message.Content)
	}
	denied := false
	for _, event := range events {
		if event.Kind == EventTool && event.ToolIsError && strings.Contains(event.ToolOutput, "Permission denied") {
			denied = true
		}
	}
	if !denied {
		t.Fatal("the denied tool left no visible error event")
	}
	if !CopilotToolsDenied(events) {
		t.Fatal("CopilotToolsDenied did not notice the denial")
	}
	if CopilotToolsDenied(copilotFixtureEvents(t, "tool.jsonl")) {
		t.Fatal("a successful tool run was reported as denied")
	}
}
