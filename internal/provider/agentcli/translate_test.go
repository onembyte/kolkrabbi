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
