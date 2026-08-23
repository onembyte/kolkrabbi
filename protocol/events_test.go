package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDeltaEventContractsMatchSchemasAndGoldens(t *testing.T) {
	tests := []struct {
		name     string
		typeName EventType
		wantText string
		decode   func(json.RawMessage) (string, error)
	}{
		{
			name:     "message.delta",
			typeName: EventMessageDelta,
			wantText: "hello from kolk",
			decode: func(raw json.RawMessage) (string, error) {
				var data MessageDeltaData
				err := json.Unmarshal(raw, &data)
				return data.Text, err
			},
		},
		{
			name:     "reasoning.delta",
			typeName: EventReasoningDelta,
			wantText: "checking the workspace",
			decode: func(raw json.RawMessage) (string, error) {
				var data ReasoningDeltaData
				err := json.Unmarshal(raw, &data)
				return data.Text, err
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if string(tc.typeName) != tc.name {
				t.Fatalf("event constant = %q, want schema and fixture name %q", tc.typeName, tc.name)
			}
			assertDeltaSchema(t, tc.name)

			want, err := os.ReadFile(filepath.Join("..", "spec", "testdata", "events", tc.name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			want = bytes.TrimSpace(want)
			envelope, err := Decode(want)
			if err != nil {
				t.Fatalf("Decode(golden): %v", err)
			}
			if envelope.Type != tc.typeName {
				t.Errorf("golden type = %q, want %q", envelope.Type, tc.typeName)
			}
			text, err := tc.decode(envelope.Data)
			if err != nil || text != tc.wantText {
				t.Errorf("typed payload text = %q, err = %v", text, err)
			}
			got, err := Encode(envelope)
			if err != nil {
				t.Fatalf("Encode(golden): %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("round trip drifted\n got: %s\nwant: %s", got, want)
			}
		})
	}
}

func TestDeltaEventsRejectInvalidText(t *testing.T) {
	for _, event := range []EventType{EventMessageDelta, EventReasoningDelta} {
		for name, data := range map[string]string{
			"missing":    `{}`,
			"empty":      `{"text":""}`,
			"null":       `{"text":null}`,
			"non-string": `{"text":1}`,
		} {
			t.Run(string(event)+"/"+name, func(t *testing.T) {
				frame := `{"seq":1,"ts":"2026-08-23T18:30:12Z","session":"` + goldenSession +
					`","turn":"` + goldenTurn + `","type":"` + string(event) + `","data":` + data + `}`
				if got, err := Decode([]byte(frame)); err == nil {
					t.Errorf("Decode accepted invalid known payload: %#v", got)
				}
			})
		}
	}
}

func assertDeltaSchema(t *testing.T, name string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "spec", "schemas", "events", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Dialect              string `json:"$schema"`
		ID                   string `json:"$id"`
		Title                string `json:"title"`
		Type                 string `json:"type"`
		Required             []string
		AdditionalProperties bool `json:"additionalProperties"`
		Properties           map[string]struct {
			Type      string `json:"type"`
			MinLength int    `json:"minLength"`
		} `json:"properties"`
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
	if !reflect.DeepEqual(schema.Required, []string{"text"}) {
		t.Errorf("required = %v, want [text]", schema.Required)
	}
	if text := schema.Properties["text"]; text.Type != "string" || text.MinLength != 1 {
		t.Errorf("text schema = %#v, want non-empty string", text)
	}
	if !strings.HasSuffix(schema.ID, "/"+name+".json") {
		t.Errorf("schema filename and id drifted: %q", schema.ID)
	}
}
