package tui

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/protocol"
)

func TestPlainRendererStreamsMessageDeltas(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlainRenderer(&buf)

	e1 := protocol.Envelope{
		Seq:       1,
		Timestamp: time.Now().UTC(),
		Session:   "s_01JQZK9XW80000000000000000",
		Turn:      "t_01JQZK9XW80000000000000001",
		Type:      protocol.EventMessageDelta,
		Data:      json.RawMessage(`{"text":"Hello, "}`),
	}
	e2 := protocol.Envelope{
		Seq:       2,
		Timestamp: time.Now().UTC(),
		Session:   "s_01JQZK9XW80000000000000000",
		Turn:      "t_01JQZK9XW80000000000000001",
		Type:      protocol.EventMessageDelta,
		Data:      json.RawMessage(`{"text":"world!"}`),
	}

	if err := r.RenderEvent(e1); err != nil {
		t.Fatalf("RenderEvent 1: %v", err)
	}
	if err := r.RenderEvent(e2); err != nil {
		t.Fatalf("RenderEvent 2: %v", err)
	}

	if got := buf.String(); got != "Hello, world!" {
		t.Fatalf("rendered output = %q, want %q", got, "Hello, world!")
	}
}

func TestPlainRendererRendersToolExecutionAndUsage(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlainRenderer(&buf)

	toolReq := protocol.Envelope{
		Seq:       1,
		Timestamp: time.Now().UTC(),
		Session:   "s_01JQZK9XW80000000000000000",
		Turn:      "t_01JQZK9XW80000000000000001",
		Type:      protocol.EventToolRequested,
		Data:      json.RawMessage(`{"id":"c1","name":"read_file","arguments":"{}","executor":"kolk"}`),
	}

	cost := 0.0012
	inToks := int64(100)
	outToks := int64(50)
	duration := int64(450)
	usage := protocol.Envelope{
		Seq:       2,
		Timestamp: time.Now().UTC(),
		Session:   "s_01JQZK9XW80000000000000000",
		Turn:      "t_01JQZK9XW80000000000000001",
		Type:      protocol.EventUsageReported,
		Data: json.RawMessage(`{
			"model":"anthropic/claude-sonnet-4.6",
			"provider_name":"openrouter",
			"request_model":"anthropic/claude-sonnet-4.6",
			"cost_source":"reported",
			"measurement":"metered",
			"attempt":1,
			"role":"main",
			"effort":"standard",
			"input_tokens":` + jsonMarshal(inToks) + `,
			"output_tokens":` + jsonMarshal(outToks) + `,
			"ttft_ms":` + jsonMarshal(duration) + `,
			"cost_usd":` + jsonMarshal(cost) + `
		}`),
	}

	if err := r.RenderEvent(toolReq); err != nil {
		t.Fatalf("RenderEvent toolReq: %v", err)
	}
	if err := r.RenderEvent(usage); err != nil {
		t.Fatalf("RenderEvent usage: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "read_file") {
		t.Fatalf("missing tool name in output: %s", out)
	}
	if !strings.Contains(out, "claude-sonnet-4.6") {
		t.Fatalf("missing model name in usage output: %s", out)
	}
	if !strings.Contains(out, "150 tok") || !strings.Contains(out, "$0.0012") {
		t.Fatalf("missing tokens or cost in usage output: %s", out)
	}
}

func jsonMarshal(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
