package protocol

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const (
	toolWorkTask  = "k_01ARYZ6S41TSV4RRFFQ69G5FAX"
	toolWorkChild = "t_01ARYZ6S41TSV4RRFFQ69G5FAY"
)

func TestToolEventsAcceptPairedSubagentWorkCorrelation(t *testing.T) {
	cases := []struct {
		event EventType
		data  any
	}{
		{EventToolRequested, ToolRequestedData{ID: "call_1", Name: "read_file", Arguments: `{}`, Executor: ToolExecutorKolk, TaskID: toolWorkTask, ChildTurn: toolWorkChild}},
		{EventToolStarted, ToolStartedData{ID: "call_1", Executor: ToolExecutorKolk, TaskID: toolWorkTask, ChildTurn: toolWorkChild}},
		{EventToolOutput, ToolOutputData{ID: "call_1", Output: "done", Executor: ToolExecutorKolk, TaskID: toolWorkTask, ChildTurn: toolWorkChild}},
		{EventToolFinished, ToolFinishedData{ID: "call_1", OK: true, Executor: ToolExecutorKolk, TaskID: toolWorkTask, ChildTurn: toolWorkChild}},
	}
	for _, tc := range cases {
		t.Run(string(tc.event), func(t *testing.T) {
			raw, err := json.Marshal(tc.data)
			if err != nil {
				t.Fatal(err)
			}
			envelope, err := Decode(toolWorkFrame(tc.event, raw))
			if err != nil {
				t.Fatalf("Decode(%s): %v", raw, err)
			}
			var correlation struct {
				TaskID    string `json:"task_id"`
				ChildTurn string `json:"child_turn"`
			}
			if err := json.Unmarshal(envelope.Data, &correlation); err != nil {
				t.Fatal(err)
			}
			if correlation.TaskID != toolWorkTask || correlation.ChildTurn != toolWorkChild {
				t.Fatalf("correlation = %+v", correlation)
			}
		})
	}
}

func TestToolEventsRejectPartialOrInvalidSubagentWorkCorrelation(t *testing.T) {
	base := map[EventType]map[string]any{
		EventToolRequested: {"id": "call_1", "name": "read_file", "arguments": `{}`, "executor": "kolk"},
		EventToolStarted:   {"id": "call_1", "executor": "kolk"},
		EventToolOutput:    {"id": "call_1", "output": "done", "executor": "kolk"},
		EventToolFinished:  {"id": "call_1", "ok": true, "executor": "kolk"},
	}
	for event, fields := range base {
		for name, mutate := range map[string]func(map[string]any){
			"task only":  func(data map[string]any) { data["task_id"] = toolWorkTask },
			"child only": func(data map[string]any) { data["child_turn"] = toolWorkChild },
			"bad task": func(data map[string]any) {
				data["task_id"] = "k_not-a-ulid"
				data["child_turn"] = toolWorkChild
			},
			"bad child": func(data map[string]any) {
				data["task_id"] = toolWorkTask
				data["child_turn"] = "t_not-a-ulid"
			},
		} {
			t.Run(string(event)+"/"+name, func(t *testing.T) {
				data := make(map[string]any, len(fields)+2)
				for key, value := range fields {
					data[key] = value
				}
				mutate(data)
				raw, err := json.Marshal(data)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := Decode(toolWorkFrame(event, raw)); err == nil {
					t.Fatalf("Decode accepted invalid correlation: %s", raw)
				}
			})
		}
	}
}

func TestToolEventSchemasDescribeOptionalSubagentWorkCorrelation(t *testing.T) {
	for _, name := range []string{"tool.requested", "tool.started", "tool.output", "tool.finished"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "spec", "schemas", "events", name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			var schema struct {
				Properties map[string]struct {
					Type      string `json:"type"`
					MinLength *int   `json:"minLength"`
				} `json:"properties"`
			}
			if err := json.Unmarshal(raw, &schema); err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{"task_id", "child_turn"} {
				property, ok := schema.Properties[field]
				if !ok || property.Type != "string" || property.MinLength == nil || *property.MinLength != 1 {
					t.Fatalf("%s.%s schema = %+v, want optional non-empty string", name, field, property)
				}
			}
		})
	}
}

func toolWorkFrame(event EventType, data []byte) []byte {
	return []byte(fmt.Sprintf(`{"seq":1,"ts":"2026-08-31T00:00:00Z","session":"%s","turn":"%s","type":"%s","data":%s}`,
		goldenSession, goldenTurn, event, data))
}
