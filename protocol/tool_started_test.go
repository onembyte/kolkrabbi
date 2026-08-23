package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestToolStartedContractMatchesSchemaAndGolden(t *testing.T) {
	if string(EventToolStarted) != "tool.started" {
		t.Fatalf("event constant = %q, want schema and fixture name tool.started", EventToolStarted)
	}
	assertToolStartedSchema(t)

	wantFrame, err := os.ReadFile(filepath.Join("..", "spec", "testdata", "events", "tool.started.json"))
	if err != nil {
		t.Fatal(err)
	}
	wantFrame = bytes.TrimSpace(wantFrame)
	envelope, err := Decode(wantFrame)
	if err != nil {
		t.Fatalf("Decode(golden): %v", err)
	}
	if envelope.Type != EventToolStarted {
		t.Errorf("golden type = %q, want %q", envelope.Type, EventToolStarted)
	}
	var data ToolStartedData
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatal(err)
	}
	wantData := ToolStartedData{ID: "call_abc123", Executor: ToolExecutorKolk}
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

func TestToolStartedRequiresCorrelationAndKnownExecutor(t *testing.T) {
	valid := map[string]any{"id": "call_1", "executor": "kolk"}
	for name, mutate := range map[string]func(map[string]any){
		"id/missing":       func(data map[string]any) { delete(data, "id") },
		"id/empty":         func(data map[string]any) { data["id"] = "" },
		"id/null":          func(data map[string]any) { data["id"] = nil },
		"id/non-string":    func(data map[string]any) { data["id"] = 1 },
		"executor/missing": func(data map[string]any) { delete(data, "executor") },
		"executor/empty":   func(data map[string]any) { data["executor"] = "" },
		"executor/null":    func(data map[string]any) { data["executor"] = nil },
		"executor/non-string": func(data map[string]any) {
			data["executor"] = true
		},
		"executor/unknown": func(data map[string]any) { data["executor"] = "vendor" },
	} {
		t.Run(name, func(t *testing.T) {
			data := cloneToolStartedData(valid)
			mutate(data)
			if got, err := Decode(toolStartedFrame(t, data)); err == nil {
				t.Errorf("Decode accepted invalid tool.started payload: %#v", got)
			}
		})
	}
}

func TestToolStartedAcceptsBothExecutorsAndRetainsUnknownFields(t *testing.T) {
	for _, executor := range []string{"kolk", "provider"} {
		t.Run(executor, func(t *testing.T) {
			got, err := Decode(toolStartedFrame(t, map[string]any{
				"id":       "toolu_01",
				"executor": executor,
				"future":   "tilbúið 🐙",
			}))
			if err != nil {
				t.Fatalf("Decode rejected executor %q: %v", executor, err)
			}
			if !bytes.Contains(got.Data, []byte(`"future":"tilbúið 🐙"`)) {
				t.Errorf("unknown tool-started field was not retained: %s", got.Data)
			}
		})
	}
}

func TestToolStartedTypedPayloadUsesSchemaOrder(t *testing.T) {
	raw, err := json.Marshal(ToolStartedData{ID: "call_1", Executor: ToolExecutorProvider})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"call_1","executor":"provider"}`
	if string(raw) != want {
		t.Fatalf("typed payload = %s, want %s", raw, want)
	}
	if _, err := Decode(toolStartedRawFrame(raw)); err != nil {
		t.Fatalf("typed payload does not satisfy its wire contract: %v", err)
	}
}

func cloneToolStartedData(source map[string]any) map[string]any {
	clone := make(map[string]any, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func toolStartedFrame(t *testing.T, data map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return toolStartedRawFrame(raw)
}

func toolStartedRawFrame(raw []byte) []byte {
	return []byte(`{"seq":1,"ts":"2026-08-23T20:31:03Z","session":"` + goldenSession +
		`","turn":"` + goldenTurn + `","type":"tool.started","data":` + string(raw) + `}`)
}

func assertToolStartedSchema(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "spec", "schemas", "events", "tool.started.json"))
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
	wantID := "https://kolkrabbi.francomichetti.com/spec/0/schemas/events/tool.started.json"
	if schema.Dialect != "https://json-schema.org/draft/2020-12/schema" || schema.ID != wantID {
		t.Errorf("schema identity = (%q, %q), want draft 2020-12 and %q", schema.Dialect, schema.ID, wantID)
	}
	if schema.Title != "tool.started payload" || schema.Type != "object" || !schema.AdditionalProperties {
		t.Error("schema root does not define a forward-compatible tool.started payload")
	}
	wantRequired := []string{"id", "executor"}
	if !reflect.DeepEqual(schema.Required, wantRequired) || len(schema.Properties) != len(wantRequired) {
		t.Errorf("schema fields = required %v, properties %v", schema.Required, schema.Properties)
	}
	id := schema.Properties["id"]
	if id.Type != "string" || id.MinLength == nil || *id.MinLength != 1 || id.Enum != nil {
		t.Errorf("id schema = %#v, want a non-empty unconstrained string", id)
	}
	executor := schema.Properties["executor"]
	if executor.Type != "string" || executor.MinLength != nil || !reflect.DeepEqual(executor.Enum, []string{"kolk", "provider"}) {
		t.Errorf("executor schema = %#v, want the kolk/provider string enum", executor)
	}
}
