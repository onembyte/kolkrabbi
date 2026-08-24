package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestTurnCancelCommandMatchesSchemaAndGolden(t *testing.T) {
	if string(CommandTurnCancel) != "turn.cancel" {
		t.Fatalf("command constant = %q, want schema and fixture name turn.cancel", CommandTurnCancel)
	}
	assertTurnCancelCommandSchema(t)

	wantJSON, err := os.ReadFile(filepath.Join("..", "spec", "testdata", "commands", "turn.cancel.json"))
	if err != nil {
		t.Fatal(err)
	}
	wantJSON = bytes.TrimSpace(wantJSON)
	if err := validateTurnCancelCommand(wantJSON); err != nil {
		t.Fatalf("validateTurnCancelCommand(golden): %v", err)
	}
	var got TurnCancelCommand
	if err := json.Unmarshal(wantJSON, &got); err != nil {
		t.Fatal(err)
	}
	want := TurnCancelCommand{TurnID: goldenTurn}
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

func TestTurnCancelCommandRequiresCanonicalTurnID(t *testing.T) {
	valid := map[string]any{"turn_id": goldenTurn}
	for name, value := range map[string]any{
		"missing": nil,
		"empty":   "",
		"null":    nil,
		"number":  1,
		"session": goldenSession,
		"task":    "k_01ARYZ6S41TSV4RRFFQ69G5FAV",
		"lower":   "t_01aryz6s41tsv4rrffq69g5faw",
		"short":   "t_01ARYZ6S41TSV4RRFFQ69G5FA",
	} {
		t.Run(name, func(t *testing.T) {
			data := map[string]any{"turn_id": valid["turn_id"]}
			if name == "missing" {
				delete(data, "turn_id")
			} else {
				data["turn_id"] = value
			}
			if err := validateTurnCancelCommand(turnCancelCommandJSON(t, data)); err == nil {
				t.Errorf("accepted invalid turn.cancel command: %#v", data)
			}
		})
	}

	data := map[string]any{"turn_id": goldenTurn, "future": true}
	if err := validateTurnCancelCommand(turnCancelCommandJSON(t, data)); err != nil {
		t.Fatalf("rejected additive command field: %v", err)
	}
}

func turnCancelCommandJSON(t *testing.T, data map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func assertTurnCancelCommandSchema(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "spec", "schemas", "commands", "turn.cancel.json"))
	if err != nil {
		t.Fatal(err)
	}
	type property struct {
		Type    string `json:"type"`
		Pattern string `json:"pattern"`
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
	wantID := "https://kolkrabbi.francomichetti.com/spec/0/schemas/commands/turn.cancel.json"
	if schema.Dialect != "https://json-schema.org/draft/2020-12/schema" || schema.ID != wantID ||
		schema.Title != "turn.cancel command" || schema.Type != "object" || !schema.AdditionalProperties {
		t.Error("schema root does not define the forward-compatible turn.cancel command")
	}
	if !reflect.DeepEqual(schema.Required, []string{"turn_id"}) || len(schema.Properties) != 1 {
		t.Errorf("schema fields = required %v, properties %v", schema.Required, schema.Properties)
	}
	wantPattern := `^t_[0-7][0-9A-HJKMNP-TV-Z]{25}$`
	if got := schema.Properties["turn_id"]; got.Type != "string" || got.Pattern != wantPattern {
		t.Errorf("turn_id schema = %#v, want canonical turn ID", got)
	}
	for _, forbidden := range []string{"session", "session_id", "reason", "status", "cancelled_at"} {
		if _, present := schema.Properties[forbidden]; present {
			t.Errorf("command schema defines server-owned field %q", forbidden)
		}
	}
}
