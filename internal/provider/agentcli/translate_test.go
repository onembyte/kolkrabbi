package agentcli

import (
	"strings"
	"testing"
)

func TestTranslateAllowListsAssistantAndResult(t *testing.T) {
	line := []byte(`{"type":"assistant","message":{"model":"claude-opus","content":[{"type":"text","text":"hello"}],"usage":{"input_tokens":2,"output_tokens":4}}}`)
	events, err := Translate(line)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Kind != EventMessageDelta || events[0].Text != "hello" ||
		events[1].Kind != EventUsage || events[1].OutputTokens != 4 {
		t.Fatalf("translated events = %+v", events)
	}
}

func TestTranslateDropsHooksAndAuthFrames(t *testing.T) {
	for _, line := range []string{
		`{"type":"system","subtype":"hook_started","stderr":"sk-ant-secret"}`,
		`{"type":"auth_status","access_token":"secret"}`,
	} {
		events, err := Translate([]byte(line))
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 0 {
			t.Fatalf("sensitive frame produced events: %+v", events)
		}
	}
}

func TestTranslateRedactsDisplayText(t *testing.T) {
	events, err := Translate([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"token sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || strings.Contains(events[0].Text, "sk-ant-") {
		t.Fatalf("unredacted event = %+v", events)
	}
}

// A user frame carrying tool_result blocks is the vendor reporting what it
// ran — with string content, array-of-blocks content, an error mark, and a
// scrub of whatever leaked into the output along the way.
func TestTranslateProjectsProviderToolResults(t *testing.T) {
	t.Run("string content", func(t *testing.T) {
		events, err := Translate([]byte(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"README.md: 12 lines"}]}}`))
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 1 || events[0].Kind != EventTool ||
			events[0].ToolName != "" || events[0].ToolCallID != "toolu_1" ||
			events[0].ToolOutput != "README.md: 12 lines" || events[0].ToolIsError {
			t.Fatalf("tool result event = %+v", events)
		}
	})
	t.Run("block content", func(t *testing.T) {
		events, err := Translate([]byte(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_2","content":[{"type":"text","text":"line one"},{"type":"text","text":"line two"}]}]}}`))
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 1 || events[0].ToolOutput != "line one\nline two" {
			t.Fatalf("flattened output = %+v", events)
		}
	})
	t.Run("error result", func(t *testing.T) {
		events, err := Translate([]byte(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_3","content":"file not found","is_error":true}]}}`))
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 1 || !events[0].ToolIsError {
			t.Fatalf("error flag = %+v", events)
		}
	})
	t.Run("redacted output", func(t *testing.T) {
		events, err := Translate([]byte(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_4","content":"token sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789"}]}}`))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(events[0].ToolOutput, "sk-ant-") {
			t.Fatalf("unredacted tool output = %+v", events[0])
		}
	})
}

// Translate can only know the tool's name from the tool_use that preceded it;
// a user frame's blocks repeat nothing but the id.
func TestTranslateToolResultsNameOnlyTheId(t *testing.T) {
	events, err := Translate([]byte(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_9","content":"ok"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ToolName != "" || events[0].ToolCallID != "toolu_9" {
		t.Fatalf("event = %+v, want the id alone", events)
	}
}

// rate_limit_event is the plan's own scarce resource speaking. "allowed"
// carries no news, a warning does, and a rejection is the cause of a failure
// that arrives one frame later.
func TestTranslateProjectsRateLimitEvents(t *testing.T) {
	t.Run("allowed is dropped", func(t *testing.T) {
		events, err := Translate([]byte(`{"type":"rate_limit_event","status":"allowed","rate_limit_type":"seven_day","utilization":0.4,"resets_at":1788220800}`))
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 0 {
			t.Fatalf("allowed events = %+v, want none", events)
		}
	})
	t.Run("warning keeps the window", func(t *testing.T) {
		events, err := Translate([]byte(`{"type":"rate_limit_event","status":"allowed_warning","rate_limit_type":"seven_day","utilization":0.78,"resets_at":1788220800}`))
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 1 || events[0].Kind != EventLimit || events[0].LimitRejected ||
			events[0].LimitWindow != "seven_day" || events[0].LimitUtilization != 0.78 ||
			events[0].LimitResets != 1788220800 {
			t.Fatalf("warning event = %+v", events)
		}
	})
	t.Run("rejection is marked", func(t *testing.T) {
		events, err := Translate([]byte(`{"type":"rate_limit_event","status":"rejected","rate_limit_type":"five_hour","resets_at":1788220800}`))
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 1 || !events[0].LimitRejected {
			t.Fatalf("rejected event = %+v", events)
		}
	})
}

// Error subtypes name their prose in errors[] and often have no result field
// at all; error_max_turns is a truncated answer, not a broken turn.
func TestTranslateErrorSubtypesUseTheErrorsField(t *testing.T) {
	events, err := Translate([]byte(`{"type":"result","subtype":"error_max_turns","errors":["Reached maximum number of turns (8)"],"is_error":true}`))
	if err != nil {
		t.Fatal(err)
	}
	errEvent := events[len(events)-1]
	if errEvent.Kind != EventError {
		t.Fatalf("last event = %+v, want the error", errEvent)
	}
	if !strings.Contains(errEvent.Error, "Reached maximum number of turns") ||
		!strings.Contains(errEvent.Error, "partial answer") {
		t.Fatalf("error = %q, want the vendor prose and the truncation named", errEvent.Error)
	}
}

func TestTranslateProjectsProviderToolUseWithoutExecutingIt(t *testing.T) {
	events, err := Translate([]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_1","name":"Read","input":{"path":"README.md"}}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != EventTool ||
		events[0].ToolName != "Read" || events[0].ToolInput != `{"path":"README.md"}` {
		t.Fatalf("tool event = %+v", events)
	}
}
