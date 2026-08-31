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

func TestPlainRendererRendersWorkMilestonesInSuppliedJournalOrder(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlainRenderer(&buf)
	r.color = false
	frames := []protocol.WorkUpdatedData{
		{ID: "t_01ARYZ6S41TSV4RRFFQ69G5FAW", Role: protocol.WorkRoleMain,
			State: protocol.WorkStateWorking, Phase: protocol.WorkPhasePlanning, Step: "planning tasks", Sequence: 1},
		{ID: "k_01ARYZ6S41TSV4RRFFQ69G5FAV", ChildTurn: "t_01ARYZ6S41TSV4RRFFQ69G5FAX", Role: protocol.WorkRoleSubagent,
			State: protocol.WorkStateWaiting, Phase: protocol.WorkPhaseSchedule, Step: "waiting for task 1", Sequence: 2,
			Index: 2, Total: 2, Model: "gpt-5.6-luna", Effort: "high"},
		{ID: "k_01ARYZ6S41TSV4RRFFQ69G5FAY", ChildTurn: "t_01ARYZ6S41TSV4RRFFQ69G5FAZ", Role: protocol.WorkRoleSubagent,
			State: protocol.WorkStateWorking, Phase: protocol.WorkPhaseProvider, Step: "model is responding", Sequence: 1,
			Index: 1, Total: 2, Model: "gpt-5.6-sol", Effort: "medium"},
	}
	for sequence, data := range frames {
		raw, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.RenderEvent(protocol.Envelope{Seq: uint64(sequence + 1), Type: protocol.EventWorkUpdated, Data: raw}); err != nil {
			t.Fatal(err)
		}
	}
	want := "◆ main · planning · working: planning tasks\n" +
		"agent [2/2] · gpt-5.6-luna · high · waiting: waiting for task 1\n" +
		"agent [1/2] · gpt-5.6-sol · medium · working: model is responding\n"
	if got := buf.String(); got != want {
		t.Fatalf("work milestone replay = %q, want %q", got, want)
	}
}

func TestWorkMilestoneSanitizesAndBoundsHostileDurableText(t *testing.T) {
	line := formatWorkUpdatedLine(protocol.WorkUpdatedData{
		ID: "k_01ARYZ6S41TSV4RRFFQ69G5FAV", ChildTurn: "t_01ARYZ6S41TSV4RRFFQ69G5FAX",
		Role: protocol.WorkRoleSubagent, State: protocol.WorkStateFailed, Phase: protocol.WorkPhaseComplete,
		Index: 1, Total: 1, Model: "gpt-5.6-luna\x1b[31m", Effort: "medium\n",
		Step: "\x1b]8;;https://example.invalid\aunsafe\x1b]8;;\a\n" + strings.Repeat("detail ", 80),
	})
	if strings.Contains(line, "\x1b") || strings.ContainsAny(line, "\r\n") || len([]rune(line)) > maxAgentStatusRunes {
		t.Fatalf("hostile durable milestone escaped its boundary: %q", line)
	}
	if !strings.Contains(line, "unsafe") || !strings.Contains(line, "failed") {
		t.Fatalf("hostile durable milestone lost its safe meaning: %q", line)
	}
}

func jsonMarshal(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
