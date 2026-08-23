package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestTurnLifecycleContractsMatchSchemasAndGoldens(t *testing.T) {
	tests := []struct {
		name     string
		typeName EventType
		want     any
		decode   func(json.RawMessage) (any, error)
	}{
		{
			name:     "turn.started",
			typeName: EventTurnStarted,
			want: TurnStartedData{
				Input: "fix the failing protocol test", Model: "openrouter/auto", Mode: "code", Effort: "standard",
			},
			decode: func(raw json.RawMessage) (any, error) {
				var data TurnStartedData
				err := json.Unmarshal(raw, &data)
				return data, err
			},
		},
		{
			name:     "turn.finished",
			typeName: EventTurnFinished,
			want:     TurnFinishedData{Reason: "stop", RawReason: "end_turn"},
			decode: func(raw json.RawMessage) (any, error) {
				var data TurnFinishedData
				err := json.Unmarshal(raw, &data)
				return data, err
			},
		},
		{
			name:     "turn.cancelled",
			typeName: EventTurnCancelled,
			want:     TurnCancelledData{Reason: "user"},
			decode: func(raw json.RawMessage) (any, error) {
				var data TurnCancelledData
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
			assertTurnLifecycleSchema(t, tc.name)

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

func TestTurnStartedRejectsInvalidProjectionFields(t *testing.T) {
	valid := map[string]any{
		"input": "fix the failing protocol test", "model": "openrouter/auto", "mode": "code", "effort": "standard",
	}
	for _, field := range []string{"input", "model", "mode", "effort"} {
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
				data := cloneTurnData(valid)
				if variant.name == "missing" {
					delete(data, field)
				} else {
					data[field] = variant.value
				}
				if got, err := Decode(turnLifecycleFrame(t, EventTurnStarted, data)); err == nil {
					t.Errorf("Decode accepted invalid turn.started payload: %#v", got)
				}
			})
		}
	}
}

func TestTurnFinishedValidatesOpenReasonsAndRetainsUnknownFields(t *testing.T) {
	for name, data := range map[string]map[string]any{
		"missing reason":        {},
		"empty reason":          {"reason": ""},
		"null reason":           {"reason": nil},
		"non-string reason":     {"reason": 1},
		"empty raw reason":      {"reason": "stop", "raw_reason": ""},
		"null raw reason":       {"reason": "stop", "raw_reason": nil},
		"non-string raw reason": {"reason": "stop", "raw_reason": 1},
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := Decode(turnLifecycleFrame(t, EventTurnFinished, data)); err == nil {
				t.Errorf("Decode accepted invalid turn.finished payload: %#v", got)
			}
		})
	}

	got, err := Decode(turnLifecycleFrame(t, EventTurnFinished, map[string]any{
		"reason": "future.reason", "future": true,
	}))
	if err != nil {
		t.Fatalf("Decode restricted an additive finish reason or field: %v", err)
	}
	if !bytes.Contains(got.Data, []byte(`"future":true`)) {
		t.Errorf("unknown finish field was not retained: %s", got.Data)
	}
}

func TestTurnFinishedTypedPayloadOmitsAbsentRawReason(t *testing.T) {
	raw, err := json.Marshal(TurnFinishedData{Reason: "stop"})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"reason":"stop"}` {
		t.Fatalf("typed finish = %s, want reason-only payload", raw)
	}
	if _, err := Decode(turnLifecycleRawFrame(EventTurnFinished, raw)); err != nil {
		t.Fatalf("typed finish does not satisfy its wire contract: %v", err)
	}
}

func TestTurnCancelledRequiresAnOpenEndedReason(t *testing.T) {
	for name, data := range map[string]map[string]any{
		"missing":    {},
		"empty":      {"reason": ""},
		"null":       {"reason": nil},
		"non-string": {"reason": 1},
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := Decode(turnLifecycleFrame(t, EventTurnCancelled, data)); err == nil {
				t.Errorf("Decode accepted invalid turn.cancelled payload: %#v", got)
			}
		})
	}
	if _, err := Decode(turnLifecycleFrame(t, EventTurnCancelled, map[string]any{"reason": "future.reason"})); err != nil {
		t.Fatalf("Decode restricted an additive cancellation reason: %v", err)
	}
}

func cloneTurnData(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func turnLifecycleFrame(t *testing.T, event EventType, data map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return turnLifecycleRawFrame(event, raw)
}

func turnLifecycleRawFrame(event EventType, raw []byte) []byte {
	return []byte(`{"seq":1,"ts":"2026-08-23T19:45:00Z","session":"` + goldenSession +
		`","turn":"` + goldenTurn + `","type":"` + string(event) + `","data":` + string(raw) + `}`)
}

func assertTurnLifecycleSchema(t *testing.T, name string) {
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
	case "turn.started":
		required = []string{"input", "model", "mode", "effort"}
		properties = required
	case "turn.finished":
		required = []string{"reason"}
		properties = []string{"reason", "raw_reason"}
	case "turn.cancelled":
		required = []string{"reason"}
		properties = required
	}
	if !reflect.DeepEqual(schema.Required, required) {
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
