package agentcli

import (
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

func TestObserveProviderEventKeepsTypedBoundaries(t *testing.T) {
	pending := make(map[string]string)
	var got []provider.ProgressEvent
	observe := func(event provider.ProgressEvent) { got = append(got, event) }

	for _, event := range []Event{
		{Kind: EventMessageDelta, Text: "thinking"},
		{Kind: EventTool, ToolCallID: "call_1", ToolName: "Bash", ToolInput: "pwd"},
		{Kind: EventTool, ToolCallID: "call_1", ToolOutput: "exit 1", ToolIsError: true},
		{Kind: EventError, Error: "provider paused"},
		{Kind: EventLimit, LimitWindow: "seven_day", LimitUtilization: 0.78},
	} {
		observeProviderEvent(observe, event, pending)
	}

	if len(got) != 5 {
		t.Fatalf("observed %d boundaries, want 5: %+v", len(got), got)
	}
	if got[0].Kind != provider.ProgressMessage || got[0].Detail != "thinking" {
		t.Fatalf("message = %+v", got[0])
	}
	if got[1].Kind != provider.ProgressToolStarted || got[1].ID != "call_1" || got[1].Name != "Bash" {
		t.Fatalf("tool start = %+v", got[1])
	}
	if got[2].Kind != provider.ProgressToolFinished || got[2].ID != "call_1" ||
		got[2].Name != "Bash" || !got[2].Error || got[2].Detail != "exit 1" {
		t.Fatalf("tool finish = %+v", got[2])
	}
	if got[3].Kind != provider.ProgressError || !got[3].Error || got[3].Detail != "provider paused" {
		t.Fatalf("error = %+v", got[3])
	}
	if got[4].Kind != provider.ProgressLimit || got[4].Error ||
		!strings.Contains(got[4].Detail, "78% of the seven-day window used") || strings.Contains(got[4].Detail, "\n") {
		t.Fatalf("limit = %+v", got[4])
	}
}
