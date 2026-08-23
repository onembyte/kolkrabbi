package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestToolOutputContractMatchesSchemaAndGolden(t *testing.T) {
	if string(EventToolOutput) != "tool.output" {
		t.Fatalf("event constant = %q, want schema and fixture name tool.output", EventToolOutput)
	}
	assertToolOutputSchema(t)

	wantFrame, err := os.ReadFile(filepath.Join("..", "spec", "testdata", "events", "tool.output.json"))
	if err != nil {
		t.Fatal(err)
	}
	wantFrame = bytes.TrimSpace(wantFrame)
	envelope, err := Decode(wantFrame)
	if err != nil {
		t.Fatalf("Decode(golden): %v", err)
	}
	if envelope.Type != EventToolOutput {
		t.Errorf("golden type = %q, want %q", envelope.Type, EventToolOutput)
	}
	var data ToolOutputData
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatal(err)
	}
	wantData := ToolOutputData{ID: "call_abc123", Output: "hi␊", Executor: ToolExecutorProvider}
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

func TestToolOutputRequiresCorrelationOutputAndKnownExecutor(t *testing.T) {
	valid := map[string]any{"id": "call_1", "output": "done", "executor": "kolk"}
	for name, mutate := range map[string]func(map[string]any){
		"id/missing":        func(data map[string]any) { delete(data, "id") },
		"id/empty":          func(data map[string]any) { data["id"] = "" },
		"id/null":           func(data map[string]any) { data["id"] = nil },
		"id/non-string":     func(data map[string]any) { data["id"] = 1 },
		"output/missing":    func(data map[string]any) { delete(data, "output") },
		"output/null":       func(data map[string]any) { data["output"] = nil },
		"output/non-string": func(data map[string]any) { data["output"] = []string{"done"} },
		"executor/missing":  func(data map[string]any) { delete(data, "executor") },
		"executor/empty":    func(data map[string]any) { data["executor"] = "" },
		"executor/null":     func(data map[string]any) { data["executor"] = nil },
		"executor/non-string": func(data map[string]any) {
			data["executor"] = true
		},
		"executor/unknown": func(data map[string]any) { data["executor"] = "vendor" },
	} {
		t.Run(name, func(t *testing.T) {
			data := cloneToolOutputData(valid)
			mutate(data)
			if got, err := Decode(toolOutputFrame(t, data)); err == nil {
				t.Errorf("Decode accepted invalid tool.output payload: %#v", got)
			}
		})
	}
}

func TestToolOutputAcceptsEmptyUnicodeBothExecutorsAndUnknownFields(t *testing.T) {
	for name, tc := range map[string]struct {
		output   string
		executor string
	}{
		"empty/kolk":       {output: "", executor: "kolk"},
		"unicode/provider": {output: "tilbúið 🐙\n", executor: "provider"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Decode(toolOutputFrame(t, map[string]any{
				"id":       "toolu_01",
				"output":   tc.output,
				"executor": tc.executor,
				"future":   "retained",
			}))
			if err != nil {
				t.Fatalf("Decode rejected valid output: %v", err)
			}
			if !bytes.Contains(got.Data, []byte(`"future":"retained"`)) {
				t.Errorf("unknown tool-output field was not retained: %s", got.Data)
			}
		})
	}
}

func TestToolOutputTypedPayloadUsesSchemaOrder(t *testing.T) {
	raw, err := json.Marshal(ToolOutputData{
		ID:       "call_1",
		Output:   "done",
		Executor: ToolExecutorKolk,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"call_1","output":"done","executor":"kolk"}`
	if string(raw) != want {
		t.Fatalf("typed payload = %s, want %s", raw, want)
	}
	if _, err := Decode(toolOutputRawFrame(raw)); err != nil {
		t.Fatalf("typed payload does not satisfy its wire contract: %v", err)
	}
}

func cloneToolOutputData(source map[string]any) map[string]any {
	clone := make(map[string]any, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func toolOutputFrame(t *testing.T, data map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return toolOutputRawFrame(raw)
}

func toolOutputRawFrame(raw []byte) []byte {
	return []byte(`{"seq":1,"ts":"2026-08-23T20:45:03Z","session":"` + goldenSession +
		`","turn":"` + goldenTurn + `","type":"tool.output","data":` + string(raw) + `}`)
}

func assertToolOutputSchema(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "spec", "schemas", "events", "tool.output.json"))
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
	wantID := "https://kolkrabbi.francomichetti.com/spec/0/schemas/events/tool.output.json"
	if schema.Dialect != "https://json-schema.org/draft/2020-12/schema" || schema.ID != wantID {
		t.Errorf("schema identity = (%q, %q), want draft 2020-12 and %q", schema.Dialect, schema.ID, wantID)
	}
	if schema.Title != "tool.output payload" || schema.Type != "object" || !schema.AdditionalProperties {
		t.Error("schema root does not define a forward-compatible tool.output payload")
	}
	wantRequired := []string{"id", "output", "executor"}
	if !reflect.DeepEqual(schema.Required, wantRequired) || len(schema.Properties) != len(wantRequired) {
		t.Errorf("schema fields = required %v, properties %v", schema.Required, schema.Properties)
	}
	id := schema.Properties["id"]
	if id.Type != "string" || id.MinLength == nil || *id.MinLength != 1 || id.Enum != nil {
		t.Errorf("id schema = %#v, want a non-empty unconstrained string", id)
	}
	output := schema.Properties["output"]
	if output.Type != "string" || output.MinLength != nil || output.Enum != nil {
		t.Errorf("output schema = %#v, want a possibly-empty unconstrained string", output)
	}
	executor := schema.Properties["executor"]
	if executor.Type != "string" || executor.MinLength != nil || !reflect.DeepEqual(executor.Enum, []string{"kolk", "provider"}) {
		t.Errorf("executor schema = %#v, want the kolk/provider string enum", executor)
	}
}
