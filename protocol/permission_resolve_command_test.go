package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPermissionResolveCommandMatchesSchemaAndGolden(t *testing.T) {
	if string(CommandPermissionResolve) != "permission.resolve" {
		t.Fatalf("command constant = %q, want schema and fixture name permission.resolve", CommandPermissionResolve)
	}
	assertPermissionResolveCommandSchema(t)

	wantJSON, err := os.ReadFile(filepath.Join("..", "spec", "testdata", "commands", "permission.resolve.json"))
	if err != nil {
		t.Fatal(err)
	}
	wantJSON = bytes.TrimSpace(wantJSON)
	if err := validatePermissionResolveCommand(wantJSON); err != nil {
		t.Fatalf("validatePermissionResolveCommand(golden): %v", err)
	}
	var got PermissionResolveCommand
	if err := json.Unmarshal(wantJSON, &got); err != nil {
		t.Fatal(err)
	}
	want := PermissionResolveCommand{ID: "perm_abc123", Decision: PermissionDecisionDeny}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("typed command = %#v, want %#v", got, want)
	}
	roundTrip, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(roundTrip, wantJSON) {
		t.Errorf("round trip drifted\n got: %s\nwant: %s", roundTrip, wantJSON)
	}
}

func TestPermissionResolveCommandValidatesCorrelationAndDecision(t *testing.T) {
	for _, decision := range []PermissionDecision{
		PermissionDecisionAllow, PermissionDecisionAllowSession, PermissionDecisionDeny,
	} {
		t.Run(string(decision), func(t *testing.T) {
			if err := validatePermissionResolveCommand(permissionResolveCommandJSON(t, map[string]any{
				"id": "perm_1", "decision": decision,
			})); err != nil {
				t.Fatalf("rejected decision %q: %v", decision, err)
			}
		})
	}

	for name, mutate := range map[string]func(map[string]any){
		"id/missing":       func(data map[string]any) { delete(data, "id") },
		"id/empty":         func(data map[string]any) { data["id"] = "" },
		"id/null":          func(data map[string]any) { data["id"] = nil },
		"id/non-string":    func(data map[string]any) { data["id"] = 1 },
		"decision/missing": func(data map[string]any) { delete(data, "decision") },
		"decision/empty":   func(data map[string]any) { data["decision"] = "" },
		"decision/null":    func(data map[string]any) { data["decision"] = nil },
		"decision/non-string": func(data map[string]any) {
			data["decision"] = true
		},
		"decision/unknown": func(data map[string]any) { data["decision"] = "allow_always" },
	} {
		t.Run(name, func(t *testing.T) {
			data := map[string]any{"id": "perm_1", "decision": "allow"}
			mutate(data)
			if err := validatePermissionResolveCommand(permissionResolveCommandJSON(t, data)); err == nil {
				t.Errorf("accepted invalid permission.resolve command: %#v", data)
			}
		})
	}

	data := map[string]any{"id": "perm_1", "decision": "deny", "future": true}
	if err := validatePermissionResolveCommand(permissionResolveCommandJSON(t, data)); err != nil {
		t.Fatalf("rejected additive command field: %v", err)
	}
}

func permissionResolveCommandJSON(t *testing.T, data map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func assertPermissionResolveCommandSchema(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "spec", "schemas", "commands", "permission.resolve.json"))
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
	wantID := "https://kolkrabbi.francomichetti.com/spec/0/schemas/commands/permission.resolve.json"
	if schema.Dialect != "https://json-schema.org/draft/2020-12/schema" || schema.ID != wantID ||
		schema.Title != "permission.resolve command" || schema.Type != "object" || !schema.AdditionalProperties {
		t.Error("schema root does not define the forward-compatible permission.resolve command")
	}
	if !reflect.DeepEqual(schema.Required, []string{"id", "decision"}) || len(schema.Properties) != 2 {
		t.Errorf("schema fields = required %v, properties %v", schema.Required, schema.Properties)
	}
	if got := schema.Properties["id"]; got.Type != "string" || got.MinLength == nil || *got.MinLength != 1 {
		t.Errorf("id schema = %#v, want non-empty string", got)
	}
	wantDecisions := []string{"allow", "allow_session", "deny"}
	if got := schema.Properties["decision"]; got.Type != "string" || !reflect.DeepEqual(got.Enum, wantDecisions) {
		t.Errorf("decision schema = %#v", got)
	}
	for _, forbidden := range []string{"reason", "tool", "detail", "diff", "expires_at"} {
		if _, present := schema.Properties[forbidden]; present {
			t.Errorf("command schema defines server-owned field %q", forbidden)
		}
	}
}
