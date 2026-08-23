package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestToolRequestedContractMatchesSchemaAndGolden(t *testing.T) {
	if string(EventToolRequested) != "tool.requested" {
		t.Fatalf("event constant = %q, want schema and fixture name tool.requested", EventToolRequested)
	}
	if ToolExecutorKolk != "kolk" || ToolExecutorProvider != "provider" {
		t.Fatalf("executor constants = (%q, %q), want (kolk, provider)", ToolExecutorKolk, ToolExecutorProvider)
	}
	assertToolRequestedSchema(t)

	wantFrame, err := os.ReadFile(filepath.Join("..", "spec", "testdata", "events", "tool.requested.json"))
	if err != nil {
		t.Fatal(err)
	}
	wantFrame = bytes.TrimSpace(wantFrame)
	envelope, err := Decode(wantFrame)
	if err != nil {
		t.Fatalf("Decode(golden): %v", err)
	}
	if envelope.Type != EventToolRequested {
		t.Errorf("golden type = %q, want %q", envelope.Type, EventToolRequested)
	}
	var data ToolRequestedData
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatal(err)
	}
	wantData := ToolRequestedData{
		ID:        "call_abc123",
		Name:      "bash",
		Arguments: `{"command":"go test ./..."}`,
		Executor:  ToolExecutorKolk,
	}
	if !reflect.DeepEqual(data, wantData) {
		t.Errorf("typed payload = %#v, want %#v", data, wantData)
	}
	gotFrame, err := Encode(envelope)
	if err != nil {
		t.Fatalf("Encode(golden): %v", err)
	}
	if !bytes.Equal(gotFrame, wantFrame) {
		t.Errorf("round trip drifted\n got: %s\nwant: %s", gotFrame, wantFrame)
	}
}

func TestToolRequestedRequiresCompleteInvocation(t *testing.T) {
	valid := map[string]any{
		"id":        "call_1",
		"name":      "read_file",
		"arguments": `{"path":"README.md"}`,
		"executor":  "kolk",
	}
	for name, mutate := range map[string]func(map[string]any){
		"id/missing":        func(data map[string]any) { delete(data, "id") },
		"id/empty":          func(data map[string]any) { data["id"] = "" },
		"id/null":           func(data map[string]any) { data["id"] = nil },
		"id/non-string":     func(data map[string]any) { data["id"] = 1 },
		"name/missing":      func(data map[string]any) { delete(data, "name") },
		"name/empty":        func(data map[string]any) { data["name"] = "" },
		"name/null":         func(data map[string]any) { data["name"] = nil },
		"name/non-string":   func(data map[string]any) { data["name"] = true },
		"args/missing":      func(data map[string]any) { delete(data, "arguments") },
		"args/empty":        func(data map[string]any) { data["arguments"] = "" },
		"args/null":         func(data map[string]any) { data["arguments"] = nil },
		"args/non-string":   func(data map[string]any) { data["arguments"] = map[string]any{} },
		"args/invalid-json": func(data map[string]any) { data["arguments"] = `{"path":` },
		"executor/missing":  func(data map[string]any) { delete(data, "executor") },
		"executor/empty":    func(data map[string]any) { data["executor"] = "" },
		"executor/null":     func(data map[string]any) { data["executor"] = nil },
		"executor/non-string": func(data map[string]any) {
			data["executor"] = []string{"kolk"}
		},
		"executor/unknown": func(data map[string]any) { data["executor"] = "vendor" },
	} {
		t.Run(name, func(t *testing.T) {
			data := cloneToolRequestedData(valid)
			mutate(data)
			if got, err := Decode(toolRequestedFrame(t, data)); err == nil {
				t.Errorf("Decode accepted invalid tool.requested payload: %#v", got)
			}
		})
	}
}

func TestToolRequestedAcceptsBothExecutorsAndRetainsUnknownFields(t *testing.T) {
	for _, executor := range []string{"kolk", "provider"} {
		t.Run(executor, func(t *testing.T) {
			got, err := Decode(toolRequestedFrame(t, map[string]any{
				"id":        "toolu_01",
				"name":      "Bash",
				"arguments": "{ \"command\" : \"printf 'tilbúið 🐙'\" }",
				"executor":  executor,
				"future":    true,
			}))
			if err != nil {
				t.Fatalf("Decode rejected executor %q: %v", executor, err)
			}
			if !bytes.Contains(got.Data, []byte(`"future":true`)) {
				t.Errorf("unknown tool-request field was not retained: %s", got.Data)
			}
			if !bytes.Contains(got.Data, []byte(`{ \"command\" :`)) {
				t.Errorf("argument text was normalized in the raw envelope: %s", got.Data)
			}
		})
	}
}

func TestToolRequestedTypedPayloadUsesSchemaOrder(t *testing.T) {
	raw, err := json.Marshal(ToolRequestedData{
		ID:        "call_1",
		Name:      "list_dir",
		Arguments: `{}`,
		Executor:  ToolExecutorProvider,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"call_1","name":"list_dir","arguments":"{}","executor":"provider"}`
	if string(raw) != want {
		t.Fatalf("typed payload = %s, want %s", raw, want)
	}
	if _, err := Decode(toolRequestedRawFrame(raw)); err != nil {
		t.Fatalf("typed payload does not satisfy its wire contract: %v", err)
	}
}

func cloneToolRequestedData(source map[string]any) map[string]any {
	clone := make(map[string]any, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func toolRequestedFrame(t *testing.T, data map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return toolRequestedRawFrame(raw)
}

func toolRequestedRawFrame(raw []byte) []byte {
	return []byte(`{"seq":1,"ts":"2026-08-23T20:18:03Z","session":"` + goldenSession +
		`","turn":"` + goldenTurn + `","type":"tool.requested","data":` + string(raw) + `}`)
}

func assertToolRequestedSchema(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "spec", "schemas", "events", "tool.requested.json"))
	if err != nil {
		t.Fatal(err)
	}
	type property struct {
		Type      string   `json:"type"`
		MinLength *int     `json:"minLength"`
		Enum      []string `json:"enum"`
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
	wantID := "https://kolkrabbi.francomichetti.com/spec/0/schemas/events/tool.requested.json"
	if schema.Dialect != "https://json-schema.org/draft/2020-12/schema" || schema.ID != wantID {
		t.Errorf("schema identity = (%q, %q), want draft 2020-12 and %q", schema.Dialect, schema.ID, wantID)
	}
	if schema.Title != "tool.requested payload" || schema.Type != "object" || !schema.AdditionalProperties {
		t.Error("schema root does not define a forward-compatible tool.requested payload")
	}
	wantRequired := []string{"id", "name", "arguments", "executor"}
	if !reflect.DeepEqual(schema.Required, wantRequired) || len(schema.Properties) != len(wantRequired) {
		t.Errorf("schema fields = required %v, properties %v", schema.Required, schema.Properties)
	}
	for _, field := range []string{"id", "name", "arguments"} {
		got := schema.Properties[field]
		if got.Type != "string" || got.MinLength == nil || *got.MinLength != 1 || got.Enum != nil {
			t.Errorf("%s schema = %#v, want a non-empty unconstrained string", field, got)
		}
	}
	executor := schema.Properties["executor"]
	if executor.Type != "string" || executor.MinLength != nil || !reflect.DeepEqual(executor.Enum, []string{"kolk", "provider"}) {
		t.Errorf("executor schema = %#v, want the kolk/provider string enum", executor)
	}
}
