package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPermissionResolvedContractMatchesSchemaAndGolden(t *testing.T) {
	if string(EventPermissionResolved) != "permission.resolved" {
		t.Fatalf("event constant = %q, want schema and fixture name permission.resolved", EventPermissionResolved)
	}
	if PermissionDecisionAllow != "allow" || PermissionDecisionAllowSession != "allow_session" || PermissionDecisionDeny != "deny" {
		t.Fatalf("decision constants = (%q, %q, %q), want (allow, allow_session, deny)",
			PermissionDecisionAllow, PermissionDecisionAllowSession, PermissionDecisionDeny)
	}
	assertPermissionResolvedSchema(t)

	wantFrame, err := os.ReadFile(filepath.Join("..", "spec", "testdata", "events", "permission.resolved.json"))
	if err != nil {
		t.Fatal(err)
	}
	wantFrame = bytes.TrimSpace(wantFrame)
	envelope, err := Decode(wantFrame)
	if err != nil {
		t.Fatalf("Decode(golden): %v", err)
	}
	if envelope.Type != EventPermissionResolved {
		t.Errorf("golden type = %q, want %q", envelope.Type, EventPermissionResolved)
	}
	var data PermissionResolvedData
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatal(err)
	}
	wantData := PermissionResolvedData{
		ID:       "perm_abc123",
		Decision: PermissionDecisionDeny,
		Reason:   "no client attached before timeout",
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

func TestPermissionResolvedRequiresIdentityAndKnownDecision(t *testing.T) {
	valid := map[string]any{"id": "perm_1", "decision": "allow"}
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
			data := clonePermissionResolvedData(valid)
			mutate(data)
			if got, err := Decode(permissionResolvedFrame(t, data)); err == nil {
				t.Errorf("Decode accepted invalid permission.resolved payload: %#v", got)
			}
		})
	}
}

func TestPermissionResolvedAcceptsAllDecisionsOptionalReasonAndUnknownFields(t *testing.T) {
	for _, decision := range []string{"allow", "allow_session", "deny"} {
		t.Run(decision, func(t *testing.T) {
			got, err := Decode(permissionResolvedFrame(t, map[string]any{
				"id": "perm_1", "decision": decision, "future": "retained",
			}))
			if err != nil {
				t.Fatalf("Decode rejected decision %q: %v", decision, err)
			}
			if !bytes.Contains(got.Data, []byte(`"future":"retained"`)) {
				t.Errorf("unknown permission-resolved field was not retained: %s", got.Data)
			}
		})
	}

	if _, err := Decode(permissionResolvedFrame(t, map[string]any{
		"id": "perm_2", "decision": "deny", "reason": "sjálfvirk höfnun 🐙",
	})); err != nil {
		t.Fatalf("Decode rejected Unicode reason: %v", err)
	}

	for name, reason := range map[string]any{"empty": "", "null": nil, "non-string": 1} {
		t.Run("reason/"+name, func(t *testing.T) {
			if got, err := Decode(permissionResolvedFrame(t, map[string]any{
				"id": "perm_1", "decision": "deny", "reason": reason,
			})); err == nil {
				t.Errorf("Decode accepted invalid reason: %#v", got)
			}
		})
	}
}

func TestPermissionResolvedTypedPayloadUsesSchemaOrderAndOmitsAbsentReason(t *testing.T) {
	raw, err := json.Marshal(PermissionResolvedData{
		ID:       "perm_1",
		Decision: PermissionDecisionDeny,
		Reason:   "timed out",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"perm_1","decision":"deny","reason":"timed out"}`
	if string(raw) != want {
		t.Fatalf("typed payload = %s, want %s", raw, want)
	}
	if _, err := Decode(permissionResolvedRawFrame(raw)); err != nil {
		t.Fatalf("typed payload does not satisfy its wire contract: %v", err)
	}

	raw, err = json.Marshal(PermissionResolvedData{ID: "perm_2", Decision: PermissionDecisionAllow})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"id":"perm_2","decision":"allow"}` {
		t.Fatalf("typed payload did not omit absent reason: %s", raw)
	}
}

func clonePermissionResolvedData(source map[string]any) map[string]any {
	clone := make(map[string]any, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func permissionResolvedFrame(t *testing.T, data map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return permissionResolvedRawFrame(raw)
}

func permissionResolvedRawFrame(raw []byte) []byte {
	return []byte(`{"seq":1,"ts":"2026-08-23T21:31:03Z","session":"` + goldenSession +
		`","turn":"` + goldenTurn + `","type":"permission.resolved","data":` + string(raw) + `}`)
}

func assertPermissionResolvedSchema(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "spec", "schemas", "events", "permission.resolved.json"))
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
	wantID := "https://kolkrabbi.francomichetti.com/spec/0/schemas/events/permission.resolved.json"
	if schema.Dialect != "https://json-schema.org/draft/2020-12/schema" || schema.ID != wantID {
		t.Errorf("schema identity = (%q, %q), want draft 2020-12 and %q", schema.Dialect, schema.ID, wantID)
	}
	if schema.Title != "permission.resolved payload" || schema.Type != "object" || !schema.AdditionalProperties {
		t.Error("schema root does not define a forward-compatible permission.resolved payload")
	}
	wantRequired := []string{"id", "decision"}
	if !reflect.DeepEqual(schema.Required, wantRequired) || len(schema.Properties) != 3 {
		t.Errorf("schema fields = required %v, properties %v", schema.Required, schema.Properties)
	}
	id := schema.Properties["id"]
	if id.Type != "string" || id.MinLength == nil || *id.MinLength != 1 || id.Enum != nil {
		t.Errorf("id schema = %#v, want a non-empty unconstrained string", id)
	}
	decision := schema.Properties["decision"]
	if decision.Type != "string" || decision.MinLength != nil || !reflect.DeepEqual(decision.Enum, []string{"allow", "allow_session", "deny"}) {
		t.Errorf("decision schema = %#v, want the closed permission-decision enum", decision)
	}
	reason := schema.Properties["reason"]
	if reason.Type != "string" || reason.MinLength == nil || *reason.MinLength != 1 || reason.Enum != nil {
		t.Errorf("reason schema = %#v, want an optional non-empty string", reason)
	}
	if _, hasTool := schema.Properties["tool"]; hasTool {
		t.Error("permission.resolved must not repeat tool")
	}
	if _, hasExecutor := schema.Properties["executor"]; hasExecutor {
		t.Error("permission.resolved must not define executor")
	}
}
