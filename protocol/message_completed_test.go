package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMessageCompletedContractMatchesSchemaAndGolden(t *testing.T) {
	if string(EventMessageCompleted) != "message.completed" {
		t.Fatalf("event constant = %q, want schema and fixture name message.completed", EventMessageCompleted)
	}
	assertMessageCompletedSchema(t)

	wantFrame, err := os.ReadFile(filepath.Join("..", "spec", "testdata", "events", "message.completed.json"))
	if err != nil {
		t.Fatal(err)
	}
	wantFrame = bytes.TrimSpace(wantFrame)
	envelope, err := Decode(wantFrame)
	if err != nil {
		t.Fatalf("Decode(golden): %v", err)
	}
	if envelope.Type != EventMessageCompleted {
		t.Errorf("golden type = %q, want %q", envelope.Type, EventMessageCompleted)
	}
	var data MessageCompletedData
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.Text != "the protocol tests are green" {
		t.Errorf("typed payload = %#v", data)
	}
	gotFrame, err := Encode(envelope)
	if err != nil {
		t.Fatalf("Encode(golden): %v", err)
	}
	if !bytes.Equal(gotFrame, wantFrame) {
		t.Errorf("round trip drifted\n got: %s\nwant: %s", gotFrame, wantFrame)
	}
}

func TestMessageCompletedRequiresTextButAllowsEmptySnapshots(t *testing.T) {
	for name, data := range map[string]map[string]any{
		"missing": {},
		"null":    {"text": nil},
		"number":  {"text": 1},
		"boolean": {"text": true},
		"array":   {"text": []string{"no"}},
		"object":  {"text": map[string]any{}},
	} {
		t.Run("reject/"+name, func(t *testing.T) {
			if got, err := Decode(messageCompletedFrame(t, data)); err == nil {
				t.Errorf("Decode accepted invalid message.completed payload: %#v", got)
			}
		})
	}

	for name, text := range map[string]string{
		"empty":   "",
		"text":    "done",
		"unicode": "tilbúið 🐙",
	} {
		t.Run("accept/"+name, func(t *testing.T) {
			got, err := Decode(messageCompletedFrame(t, map[string]any{"text": text, "future": true}))
			if err != nil {
				t.Fatalf("Decode rejected a valid finalized snapshot: %v", err)
			}
			if !bytes.Contains(got.Data, []byte(`"future":true`)) {
				t.Errorf("unknown completed-message field was not retained: %s", got.Data)
			}
		})
	}
}

func TestMessageCompletedTypedPayloadPreservesEmptyText(t *testing.T) {
	raw, err := json.Marshal(MessageCompletedData{Text: ""})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"text":""}` {
		t.Fatalf("typed empty snapshot = %s, want an explicit empty text field", raw)
	}
	if _, err := Decode(messageCompletedRawFrame(raw)); err != nil {
		t.Fatalf("typed empty snapshot does not satisfy its wire contract: %v", err)
	}
}

func messageCompletedFrame(t *testing.T, data map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return messageCompletedRawFrame(raw)
}

func messageCompletedRawFrame(raw []byte) []byte {
	return []byte(`{"seq":1,"ts":"2026-08-23T20:05:03Z","session":"` + goldenSession +
		`","turn":"` + goldenTurn + `","type":"message.completed","data":` + string(raw) + `}`)
}

func assertMessageCompletedSchema(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "spec", "schemas", "events", "message.completed.json"))
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
	wantID := "https://kolkrabbi.francomichetti.com/spec/0/schemas/events/message.completed.json"
	if schema.Dialect != "https://json-schema.org/draft/2020-12/schema" || schema.ID != wantID {
		t.Errorf("schema identity = (%q, %q), want draft 2020-12 and %q", schema.Dialect, schema.ID, wantID)
	}
	if schema.Title != "message.completed payload" || schema.Type != "object" || !schema.AdditionalProperties {
		t.Error("schema root does not define a forward-compatible message.completed payload")
	}
	if !reflect.DeepEqual(schema.Required, []string{"text"}) || len(schema.Properties) != 1 {
		t.Errorf("schema fields = required %v, properties %v", schema.Required, schema.Properties)
	}
	if got := schema.Properties["text"]; got.Type != "string" || got.MinLength != nil {
		t.Errorf("text schema = %#v, want a string with no non-empty constraint", got)
	}
}
