package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPermissionRequestedContractMatchesSchemaAndGolden(t *testing.T) {
	if string(EventPermissionRequested) != "permission.requested" {
		t.Fatalf("event constant = %q, want schema and fixture name permission.requested", EventPermissionRequested)
	}
	assertPermissionRequestedSchema(t)

	wantFrame, err := os.ReadFile(filepath.Join("..", "spec", "testdata", "events", "permission.requested.json"))
	if err != nil {
		t.Fatal(err)
	}
	wantFrame = bytes.TrimSpace(wantFrame)
	envelope, err := Decode(wantFrame)
	if err != nil {
		t.Fatalf("Decode(golden): %v", err)
	}
	if envelope.Type != EventPermissionRequested {
		t.Errorf("golden type = %q, want %q", envelope.Type, EventPermissionRequested)
	}
	var data PermissionRequestedData
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatal(err)
	}
	wantData := PermissionRequestedData{
		ID:     "perm_abc123",
		Tool:   "edit_file",
		Detail: "Edit README.md",
		Diff:   "- old\n+ new 🐙",
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

func TestPermissionRequestedRequiresIdentityToolAndDetail(t *testing.T) {
	valid := map[string]any{"id": "perm_1", "tool": "bash", "detail": "Run tests"}
	for name, mutate := range map[string]func(map[string]any){
		"id/missing":        func(data map[string]any) { delete(data, "id") },
		"id/empty":          func(data map[string]any) { data["id"] = "" },
		"id/null":           func(data map[string]any) { data["id"] = nil },
		"id/non-string":     func(data map[string]any) { data["id"] = 1 },
		"tool/missing":      func(data map[string]any) { delete(data, "tool") },
		"tool/empty":        func(data map[string]any) { data["tool"] = "" },
		"tool/null":         func(data map[string]any) { data["tool"] = nil },
		"tool/non-string":   func(data map[string]any) { data["tool"] = true },
		"detail/missing":    func(data map[string]any) { delete(data, "detail") },
		"detail/empty":      func(data map[string]any) { data["detail"] = "" },
		"detail/null":       func(data map[string]any) { data["detail"] = nil },
		"detail/non-string": func(data map[string]any) { data["detail"] = []string{"Run tests"} },
	} {
		t.Run(name, func(t *testing.T) {
			data := clonePermissionRequestedData(valid)
			mutate(data)
			if got, err := Decode(permissionRequestedFrame(t, data)); err == nil {
				t.Errorf("Decode accepted invalid permission.requested payload: %#v", got)
			}
		})
	}
}

func TestPermissionRequestedOptionalDiffAndUnknownFields(t *testing.T) {
	for name, data := range map[string]map[string]any{
		"omitted": {
			"id": "perm_1", "tool": "bash", "detail": "Run tests",
		},
		"empty": {
			"id": "perm_2", "tool": "write_file", "detail": "Write empty file", "diff": "",
		},
		"unicode": {
			"id": "perm_3", "tool": "edit_file", "detail": "Edit README", "diff": "- old\n+ nýtt 🐙",
		},
	} {
		t.Run(name, func(t *testing.T) {
			data["future"] = "retained"
			got, err := Decode(permissionRequestedFrame(t, data))
			if err != nil {
				t.Fatalf("Decode rejected valid optional diff: %v", err)
			}
			if !bytes.Contains(got.Data, []byte(`"future":"retained"`)) {
				t.Errorf("unknown permission-requested field was not retained: %s", got.Data)
			}
		})
	}

	for name, diff := range map[string]any{"null": nil, "non-string": true} {
		t.Run(name, func(t *testing.T) {
			if got, err := Decode(permissionRequestedFrame(t, map[string]any{
				"id": "perm_1", "tool": "bash", "detail": "Run tests", "diff": diff,
			})); err == nil {
				t.Errorf("Decode accepted invalid diff: %#v", got)
			}
		})
	}
}

func TestPermissionRequestedTypedPayloadUsesSchemaOrder(t *testing.T) {
	raw, err := json.Marshal(PermissionRequestedData{
		ID:     "perm_1",
		Tool:   "write_file",
		Detail: "Write config",
		Diff:   "+ enabled=true",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"perm_1","tool":"write_file","detail":"Write config","diff":"+ enabled=true"}`
	if string(raw) != want {
		t.Fatalf("typed payload = %s, want %s", raw, want)
	}
	if _, err := Decode(permissionRequestedRawFrame(raw)); err != nil {
		t.Fatalf("typed payload does not satisfy its wire contract: %v", err)
	}
}

func clonePermissionRequestedData(source map[string]any) map[string]any {
	clone := make(map[string]any, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func permissionRequestedFrame(t *testing.T, data map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return permissionRequestedRawFrame(raw)
}

func permissionRequestedRawFrame(raw []byte) []byte {
	return []byte(`{"seq":1,"ts":"2026-08-23T21:18:03Z","session":"` + goldenSession +
		`","turn":"` + goldenTurn + `","type":"permission.requested","data":` + string(raw) + `}`)
}

func assertPermissionRequestedSchema(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "spec", "schemas", "events", "permission.requested.json"))
	if err != nil {
		t.Fatal(err)
	}
	type property struct {
		Type      string `json:"type"`
		MinLength *int   `json:"minLength"`
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
	wantID := "https://kolkrabbi.francomichetti.com/spec/0/schemas/events/permission.requested.json"
	if schema.Dialect != "https://json-schema.org/draft/2020-12/schema" || schema.ID != wantID {
		t.Errorf("schema identity = (%q, %q), want draft 2020-12 and %q", schema.Dialect, schema.ID, wantID)
	}
	if schema.Title != "permission.requested payload" || schema.Type != "object" || !schema.AdditionalProperties {
		t.Error("schema root does not define a forward-compatible permission.requested payload")
	}
	wantRequired := []string{"id", "tool", "detail"}
	if !reflect.DeepEqual(schema.Required, wantRequired) || len(schema.Properties) != 4 {
		t.Errorf("schema fields = required %v, properties %v", schema.Required, schema.Properties)
	}
	for _, name := range wantRequired {
		property := schema.Properties[name]
		if property.Type != "string" || property.MinLength == nil || *property.MinLength != 1 {
			t.Errorf("%s schema = %#v, want a non-empty string", name, property)
		}
	}
	diff := schema.Properties["diff"]
	if diff.Type != "string" || diff.MinLength != nil {
		t.Errorf("diff schema = %#v, want an optional possibly-empty string", diff)
	}
	if _, hasExecutor := schema.Properties["executor"]; hasExecutor {
		t.Error("permission.requested must not define executor")
	}
}
