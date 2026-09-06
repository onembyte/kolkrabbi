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

// provider.limit is bound to its schema and its golden frame the way the turn
// lifecycle events are: the constant names the files, the golden decodes to
// the typed payload, and encoding it back is byte-identical.
func TestProviderLimitContractMatchesSchemaAndGolden(t *testing.T) {
	const name = "provider.limit"
	if string(EventProviderLimit) != name {
		t.Fatalf("event constant = %q, want %q", EventProviderLimit, name)
	}
	raw, err := os.ReadFile(filepath.Join("..", "spec", "schemas", "events", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Dialect              string                    `json:"$schema"`
		ID                   string                    `json:"$id"`
		Title                string                    `json:"title"`
		Type                 string                    `json:"type"`
		Required             []string                  `json:"required"`
		Properties           map[string]map[string]any `json:"properties"`
		AdditionalProperties bool                      `json:"additionalProperties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("schema is not JSON: %v", err)
	}
	if schema.Dialect != "https://json-schema.org/draft/2020-12/schema" || schema.ID != "https://kolkrabbi.francomichetti.com/spec/0/schemas/events/"+name+".json" {
		t.Errorf("schema identity = (%q, %q)", schema.Dialect, schema.ID)
	}
	if schema.Title != name+" payload" || schema.Type != "object" || !schema.AdditionalProperties {
		t.Errorf("schema root does not define a forward-compatible %s payload", name)
	}
	if !reflect.DeepEqual(schema.Required, []string{"kind", "scope", "action"}) {
		t.Errorf("required = %v, want kind, scope, action", schema.Required)
	}
	for _, field := range []string{"kind", "scope", "action", "model", "connector", "reset_at", "message", "source"} {
		if schema.Properties[field]["type"] != "string" {
			t.Errorf("property %s = %v, want a string", field, schema.Properties[field])
		}
	}
	if schema.Properties["retry_after_ms"]["type"] != "integer" {
		t.Errorf("retry_after_ms = %v, want an integer", schema.Properties["retry_after_ms"])
	}
	wantFrame, err := os.ReadFile(filepath.Join("..", "spec", "testdata", "events", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	wantFrame = bytes.TrimSpace(wantFrame)
	envelope, err := Decode(wantFrame)
	if err != nil {
		t.Fatalf("Decode(golden): %v", err)
	}
	var data ProviderLimitData
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatal(err)
	}
	want := ProviderLimitData{Kind: "subscription_allowance", Scope: "account", Action: "pause", Model: "claude-fable", Connector: "claude", ResetAt: "2026-09-05T20:25:00Z", Message: "usage limit reached", Source: "vendor-frame"}
	if !reflect.DeepEqual(data, want) {
		t.Fatalf("typed payload = %#v, want %#v", data, want)
	}
	gotFrame, err := Encode(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotFrame, wantFrame) {
		t.Fatalf("round trip drifted\n got: %s\nwant: %s", gotFrame, wantFrame)
	}
}

// The vocabularies are closed: a kind, scope or action outside them is refused
// at decode, so no surface has to guess what an unknown word meant.
func TestProviderLimitRefusesWordsOutsideItsVocabularies(t *testing.T) {
	golden, err := os.ReadFile(filepath.Join("..", "spec", "testdata", "events", "provider.limit.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ field, bad string }{{"kind", "banana"}, {"scope", "planet"}, {"action", "shrug"}} {
		frame := strings.Replace(string(golden), `"`+tc.field+`":"`, `"`+tc.field+`":"`+tc.bad+`x`, 1)
		frame = strings.Replace(frame, tc.bad+"x"+map[string]string{"kind": "subscription_allowance", "scope": "account", "action": "pause"}[tc.field], tc.bad, 1)
		if _, err := Decode([]byte(strings.TrimSpace(frame))); err == nil {
			t.Errorf("%s=%q was accepted", tc.field, tc.bad)
		}
	}
}
