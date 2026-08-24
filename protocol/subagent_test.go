package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const (
	goldenTask      = "k_01ARYZ6S41TSV4RRFFQ69G5FAX"
	goldenChildTurn = "t_01ARYZ6S41TSV4RRFFQ69G5FAY"
)

func TestSubagentLifecycleContractsMatchSchemasAndGoldens(t *testing.T) {
	tests := []struct {
		name     string
		typeName EventType
		want     any
		decode   func(json.RawMessage) (any, error)
	}{
		{
			name:     "subagent.started",
			typeName: EventSubagentStarted,
			want: SubagentStartedData{
				ID: goldenTask, ChildTurn: goldenChildTurn, Task: "verify the protocol contract",
				Mode: "task", Index: 1, Total: 3,
			},
			decode: func(raw json.RawMessage) (any, error) {
				var data SubagentStartedData
				err := json.Unmarshal(raw, &data)
				return data, err
			},
		},
		{
			name:     "subagent.finished",
			typeName: EventSubagentFinished,
			want: SubagentFinishedData{
				ID: goldenTask, ChildTurn: goldenChildTurn, Mode: "task", OK: true,
			},
			decode: func(raw json.RawMessage) (any, error) {
				var data SubagentFinishedData
				err := json.Unmarshal(raw, &data)
				return data, err
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if string(tc.typeName) != tc.name {
				t.Fatalf("event constant = %q, want schema and fixture name %q", tc.typeName, tc.name)
			}
			assertSubagentSchema(t, tc.name)

			wantFrame, err := os.ReadFile(filepath.Join("..", "spec", "testdata", "events", tc.name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			wantFrame = bytes.TrimSpace(wantFrame)
			envelope, err := Decode(wantFrame)
			if err != nil {
				t.Fatalf("Decode(golden): %v", err)
			}
			if envelope.Type != tc.typeName {
				t.Errorf("golden type = %q, want %q", envelope.Type, tc.typeName)
			}
			data, err := tc.decode(envelope.Data)
			if err != nil || !reflect.DeepEqual(data, tc.want) {
				t.Errorf("typed payload = %#v, want %#v, err = %v", data, tc.want, err)
			}
			gotFrame, err := Encode(envelope)
			if err != nil {
				t.Fatalf("Encode(golden): %v", err)
			}
			if !bytes.Equal(gotFrame, wantFrame) {
				t.Errorf("round trip drifted\n got: %s\nwant: %s", gotFrame, wantFrame)
			}
		})
	}
}

func TestSubagentLifecycleRequiresCorrelationAndMode(t *testing.T) {
	for _, event := range []EventType{EventSubagentStarted, EventSubagentFinished} {
		valid := validSubagentData(event)
		for _, field := range []string{"id", "child_turn", "mode"} {
			for _, variant := range []struct {
				name  string
				value any
			}{
				{name: "missing"},
				{name: "empty", value: ""},
				{name: "null", value: nil},
				{name: "non-string", value: 1},
			} {
				t.Run(string(event)+"/"+field+"/"+variant.name, func(t *testing.T) {
					data := cloneSubagentData(valid)
					if variant.name == "missing" {
						delete(data, field)
					} else {
						data[field] = variant.value
					}
					if got, err := Decode(subagentFrame(t, event, data)); err == nil {
						t.Errorf("Decode accepted invalid %s payload: %#v", event, got)
					}
				})
			}
		}
	}

	for name, mutate := range map[string]func(map[string]any){
		"id/wrong-kind":         func(data map[string]any) { data["id"] = goldenTurn },
		"id/non-canonical":      func(data map[string]any) { data["id"] = "k_task_1" },
		"child-turn/wrong-kind": func(data map[string]any) { data["child_turn"] = goldenTask },
		"child-turn/lowercase":  func(data map[string]any) { data["child_turn"] = "t_01aryz6s41tsv4rrffq69g5fay" },
	} {
		t.Run(name, func(t *testing.T) {
			for _, event := range []EventType{EventSubagentStarted, EventSubagentFinished} {
				data := validSubagentData(event)
				mutate(data)
				if got, err := Decode(subagentFrame(t, event, data)); err == nil {
					t.Errorf("Decode accepted invalid %s correlation: %#v", event, got)
				}
			}
		})
	}
}

func TestSubagentStartedRequiresTaskAndValidOrdinal(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any){
		"task/missing":      func(data map[string]any) { delete(data, "task") },
		"task/empty":        func(data map[string]any) { data["task"] = "" },
		"task/null":         func(data map[string]any) { data["task"] = nil },
		"task/non-string":   func(data map[string]any) { data["task"] = true },
		"index/missing":     func(data map[string]any) { delete(data, "index") },
		"index/zero":        func(data map[string]any) { data["index"] = 0 },
		"index/fractional":  func(data map[string]any) { data["index"] = 1.5 },
		"index/non-number":  func(data map[string]any) { data["index"] = "1" },
		"total/missing":     func(data map[string]any) { delete(data, "total") },
		"total/zero":        func(data map[string]any) { data["total"] = 0 },
		"total/fractional":  func(data map[string]any) { data["total"] = 2.5 },
		"total/non-number":  func(data map[string]any) { data["total"] = "3" },
		"index-after-total": func(data map[string]any) { data["index"], data["total"] = 4, 3 },
	} {
		t.Run(name, func(t *testing.T) {
			data := validSubagentData(EventSubagentStarted)
			mutate(data)
			if got, err := Decode(subagentFrame(t, EventSubagentStarted, data)); err == nil {
				t.Errorf("Decode accepted invalid subagent.started payload: %#v", got)
			}
		})
	}
}

func TestSubagentFinishedRequiresBooleanOutcome(t *testing.T) {
	for name, value := range map[string]any{
		"missing": nil,
		"null":    nil,
		"string":  "true",
		"number":  1,
	} {
		t.Run(name, func(t *testing.T) {
			data := validSubagentData(EventSubagentFinished)
			if name == "missing" {
				delete(data, "ok")
			} else {
				data["ok"] = value
			}
			if got, err := Decode(subagentFrame(t, EventSubagentFinished, data)); err == nil {
				t.Errorf("Decode accepted invalid subagent.finished payload: %#v", got)
			}
		})
	}

	for _, ok := range []bool{false, true} {
		data := validSubagentData(EventSubagentFinished)
		data["ok"] = ok
		got, err := Decode(subagentFrame(t, EventSubagentFinished, data))
		if err != nil {
			t.Fatalf("Decode rejected ok=%v: %v", ok, err)
		}
		if !bytes.Contains(got.Data, []byte(`"ok":`)) {
			t.Errorf("outcome was not retained: %s", got.Data)
		}
	}
}

func TestSubagentLifecycleTypedPayloadOrderAndUnknownFields(t *testing.T) {
	started, err := json.Marshal(SubagentStartedData{
		ID: goldenTask, ChildTurn: goldenChildTurn, Task: "inspect Unicode 🐙", Mode: "task", Index: 2, Total: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantStarted := `{"id":"` + goldenTask + `","child_turn":"` + goldenChildTurn + `","task":"inspect Unicode 🐙","mode":"task","index":2,"total":3}`
	if string(started) != wantStarted {
		t.Fatalf("typed started payload = %s, want %s", started, wantStarted)
	}
	if _, err := Decode(subagentRawFrame(EventSubagentStarted, started)); err != nil {
		t.Fatalf("typed started payload does not satisfy its wire contract: %v", err)
	}

	finished, err := json.Marshal(SubagentFinishedData{
		ID: goldenTask, ChildTurn: goldenChildTurn, Mode: "task", OK: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantFinished := `{"id":"` + goldenTask + `","child_turn":"` + goldenChildTurn + `","mode":"task","ok":false}`
	if string(finished) != wantFinished {
		t.Fatalf("typed finished payload = %s, want %s", finished, wantFinished)
	}
	if _, err := Decode(subagentRawFrame(EventSubagentFinished, finished)); err != nil {
		t.Fatalf("typed finished payload does not satisfy its wire contract: %v", err)
	}

	for _, event := range []EventType{EventSubagentStarted, EventSubagentFinished} {
		data := validSubagentData(event)
		data["future"] = "kept"
		got, err := Decode(subagentFrame(t, event, data))
		if err != nil {
			t.Fatalf("Decode rejected additive %s field: %v", event, err)
		}
		if !bytes.Contains(got.Data, []byte(`"future":"kept"`)) {
			t.Errorf("unknown %s field was not retained: %s", event, got.Data)
		}
	}
}

func validSubagentData(event EventType) map[string]any {
	data := map[string]any{"id": goldenTask, "child_turn": goldenChildTurn, "mode": "task"}
	if event == EventSubagentStarted {
		data["task"] = "verify the protocol contract"
		data["index"] = 1
		data["total"] = 3
	} else {
		data["ok"] = true
	}
	return data
}

func cloneSubagentData(source map[string]any) map[string]any {
	clone := make(map[string]any, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func subagentFrame(t *testing.T, event EventType, data map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return subagentRawFrame(event, raw)
}

func subagentRawFrame(event EventType, raw []byte) []byte {
	return []byte(`{"seq":1,"ts":"2026-08-23T22:01:03Z","session":"` + goldenSession +
		`","turn":"` + goldenTurn + `","type":"` + string(event) + `","data":` + string(raw) + `}`)
}

func assertSubagentSchema(t *testing.T, name string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "spec", "schemas", "events", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	type property struct {
		Type      string `json:"type"`
		Pattern   string `json:"pattern"`
		MinLength *int   `json:"minLength"`
		Minimum   *int   `json:"minimum"`
	}
	var schema struct {
		Dialect              string              `json:"$schema"`
		ID                   string              `json:"$id"`
		Title                string              `json:"title"`
		Type                 string              `json:"type"`
		Required             []string            `json:"required"`
		Properties           map[string]property `json:"properties"`
		AdditionalProperties bool                `json:"additionalProperties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("schema is not JSON: %v", err)
	}
	wantID := "https://kolkrabbi.francomichetti.com/spec/0/schemas/events/" + name + ".json"
	if schema.Dialect != "https://json-schema.org/draft/2020-12/schema" || schema.ID != wantID {
		t.Errorf("schema identity = (%q, %q), want draft 2020-12 and %q", schema.Dialect, schema.ID, wantID)
	}
	if schema.Title != name+" payload" || schema.Type != "object" || !schema.AdditionalProperties {
		t.Errorf("schema root does not define a forward-compatible %s payload", name)
	}

	wantRequired := []string{"id", "child_turn", "mode", "ok"}
	if name == "subagent.started" {
		wantRequired = []string{"id", "child_turn", "task", "mode", "index", "total"}
	}
	if !reflect.DeepEqual(schema.Required, wantRequired) || len(schema.Properties) != len(wantRequired) {
		t.Errorf("schema fields = required %v, properties %v", schema.Required, schema.Properties)
	}
	if schema.Properties["id"].Type != "string" ||
		schema.Properties["id"].Pattern != "^k_[0-7][0-9A-HJKMNP-TV-Z]{25}$" {
		t.Errorf("task id schema = %#v", schema.Properties["id"])
	}
	if schema.Properties["child_turn"].Type != "string" ||
		schema.Properties["child_turn"].Pattern != "^t_[0-7][0-9A-HJKMNP-TV-Z]{25}$" {
		t.Errorf("child turn schema = %#v", schema.Properties["child_turn"])
	}
	mode := schema.Properties["mode"]
	if mode.Type != "string" || mode.MinLength == nil || *mode.MinLength != 1 {
		t.Errorf("mode schema = %#v, want non-empty string", mode)
	}
	if name == "subagent.started" {
		task := schema.Properties["task"]
		if task.Type != "string" || task.MinLength == nil || *task.MinLength != 1 {
			t.Errorf("task schema = %#v, want non-empty string", task)
		}
		for _, field := range []string{"index", "total"} {
			value := schema.Properties[field]
			if value.Type != "integer" || value.Minimum == nil || *value.Minimum != 1 {
				t.Errorf("%s schema = %#v, want integer >= 1", field, value)
			}
		}
	} else if ok := schema.Properties["ok"]; ok.Type != "boolean" {
		t.Errorf("ok schema = %#v, want required boolean", ok)
	}
}
