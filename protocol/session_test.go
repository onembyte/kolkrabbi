package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSessionLifecycleContractsMatchSchemasAndGoldens(t *testing.T) {
	tests := []struct {
		name     string
		typeName EventType
		want     any
		decode   func(json.RawMessage) (any, error)
	}{
		{
			name:     "session.started",
			typeName: EventSessionStarted,
			want: SessionStartedData{
				Model: "openrouter/auto", Mode: "code", Effort: "standard", CWD: "/work/kolkrabbi",
			},
			decode: func(raw json.RawMessage) (any, error) {
				var data SessionStartedData
				err := json.Unmarshal(raw, &data)
				return data, err
			},
		},
		{
			name:     "session.updated",
			typeName: EventSessionUpdated,
			want: SessionUpdatedData{
				Model: "anthropic/claude-sonnet-4.6", Title: "Fix protocol lifecycle",
			},
			decode: func(raw json.RawMessage) (any, error) {
				var data SessionUpdatedData
				err := json.Unmarshal(raw, &data)
				return data, err
			},
		},
		{
			name:     "session.ended",
			typeName: EventSessionEnded,
			want:     SessionEndedData{Reason: "closed"},
			decode: func(raw json.RawMessage) (any, error) {
				var data SessionEndedData
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
			assertSessionLifecycleSchema(t, tc.name)

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

func TestSessionStartedRejectsInvalidProjectionFields(t *testing.T) {
	valid := map[string]any{
		"model": "openrouter/auto", "mode": "code", "effort": "standard", "cwd": "/work/kolkrabbi",
	}
	for _, field := range []string{"model", "mode", "effort", "cwd"} {
		for _, variant := range []struct {
			name  string
			value any
		}{
			{name: "missing"},
			{name: "empty", value: ""},
			{name: "null", value: nil},
			{name: "non-string", value: 1},
		} {
			t.Run(field+"/"+variant.name, func(t *testing.T) {
				data := cloneSessionData(valid)
				if variant.name == "missing" {
					delete(data, field)
				} else {
					data[field] = variant.value
				}
				if got, err := Decode(sessionLifecycleFrame(t, EventSessionStarted, data)); err == nil {
					t.Errorf("Decode accepted invalid session.started payload: %#v", got)
				}
			})
		}
	}
}

func TestSessionUpdatedRequiresANonEmptyValidPatch(t *testing.T) {
	if got, err := Decode(sessionLifecycleFrame(t, EventSessionUpdated, map[string]any{})); err == nil {
		t.Errorf("Decode accepted an empty session.updated patch: %#v", got)
	}

	for _, field := range []string{"model", "mode", "effort", "title"} {
		for name, value := range map[string]any{"empty": "", "null": nil, "non-string": 1} {
			t.Run(field+"/"+name, func(t *testing.T) {
				if got, err := Decode(sessionLifecycleFrame(t, EventSessionUpdated, map[string]any{field: value})); err == nil {
					t.Errorf("Decode accepted invalid session.updated payload: %#v", got)
				}
			})
		}
	}

	t.Run("unknown-only patch", func(t *testing.T) {
		got, err := Decode(sessionLifecycleFrame(t, EventSessionUpdated, map[string]any{"future_state": "ready"}))
		if err != nil {
			t.Fatalf("Decode rejected an additive unknown-only patch: %v", err)
		}
		if !bytes.Contains(got.Data, []byte(`"future_state":"ready"`)) {
			t.Errorf("unknown update field was not retained: %s", got.Data)
		}
	})
}

func TestSessionUpdatedTypedPayloadMarshalsAsAPatch(t *testing.T) {
	raw, err := json.Marshal(SessionUpdatedData{Model: "openrouter/auto"})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"model":"openrouter/auto"}` {
		t.Fatalf("typed update = %s, want one-field patch", raw)
	}
	if _, err := Decode(sessionLifecycleRawFrame(EventSessionUpdated, raw)); err != nil {
		t.Fatalf("typed update does not satisfy its wire contract: %v", err)
	}
}

func TestSessionEndedRequiresAnOpenEndedReason(t *testing.T) {
	for name, data := range map[string]map[string]any{
		"missing":    {},
		"empty":      {"reason": ""},
		"null":       {"reason": nil},
		"non-string": {"reason": 1},
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := Decode(sessionLifecycleFrame(t, EventSessionEnded, data)); err == nil {
				t.Errorf("Decode accepted invalid session.ended payload: %#v", got)
			}
		})
	}
	if _, err := Decode(sessionLifecycleFrame(t, EventSessionEnded, map[string]any{"reason": "future.reason"})); err != nil {
		t.Fatalf("Decode restricted an additive end reason: %v", err)
	}
}

func cloneSessionData(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func sessionLifecycleFrame(t *testing.T, event EventType, data map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return sessionLifecycleRawFrame(event, raw)
}

func sessionLifecycleRawFrame(event EventType, raw []byte) []byte {
	return []byte(`{"seq":1,"ts":"2026-08-23T19:30:00Z","session":"` + goldenSession +
		`","turn":"` + goldenTurn + `","type":"` + string(event) + `","data":` + string(raw) + `}`)
}

func assertSessionLifecycleSchema(t *testing.T, name string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "spec", "schemas", "events", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	type property struct {
		Type      string `json:"type"`
		MinLength int    `json:"minLength"`
	}
	var schema struct {
		Dialect              string              `json:"$schema"`
		ID                   string              `json:"$id"`
		Title                string              `json:"title"`
		Type                 string              `json:"type"`
		Required             []string            `json:"required"`
		MinProperties        int                 `json:"minProperties"`
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

	var required, properties []string
	switch name {
	case "session.started":
		required = []string{"model", "mode", "effort", "cwd"}
		properties = required
	case "session.updated":
		properties = []string{"model", "mode", "effort", "title"}
		if len(schema.Required) != 0 || schema.MinProperties != 1 {
			t.Errorf("updated patch required = %v, minProperties = %d", schema.Required, schema.MinProperties)
		}
	case "session.ended":
		required = []string{"reason"}
		properties = required
	}
	if required != nil && !reflect.DeepEqual(schema.Required, required) {
		t.Errorf("required = %v, want %v", schema.Required, required)
	}
	if len(schema.Properties) != len(properties) {
		t.Errorf("properties = %v, want exactly %v", schema.Properties, properties)
	}
	for _, field := range properties {
		if got := schema.Properties[field]; got.Type != "string" || got.MinLength != 1 {
			t.Errorf("%s schema = %#v, want non-empty string", field, got)
		}
	}
}
