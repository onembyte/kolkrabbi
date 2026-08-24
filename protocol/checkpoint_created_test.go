package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCheckpointCreatedContractMatchesSchemaAndGolden(t *testing.T) {
	if string(EventCheckpointCreated) != "checkpoint.created" {
		t.Fatalf("event constant = %q, want schema and fixture name checkpoint.created", EventCheckpointCreated)
	}
	assertCheckpointCreatedSchema(t)

	wantFrame, err := os.ReadFile(filepath.Join("..", "spec", "testdata", "events", "checkpoint.created.json"))
	if err != nil {
		t.Fatal(err)
	}
	wantFrame = bytes.TrimSpace(wantFrame)
	envelope, err := Decode(wantFrame)
	if err != nil {
		t.Fatalf("Decode(golden): %v", err)
	}
	if envelope.Type != EventCheckpointCreated {
		t.Errorf("golden type = %q, want %q", envelope.Type, EventCheckpointCreated)
	}
	var data CheckpointCreatedData
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatal(err)
	}
	wantData := CheckpointCreatedData{
		ID: "checkpoint_000001", Reason: "before_write", Tool: "edit_file",
		Path: "/work/README.md", Existed: true,
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

func TestCheckpointCreatedRequiresSnapshotIdentityAndContext(t *testing.T) {
	for _, field := range []string{"id", "reason", "tool", "path"} {
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
				data := validCheckpointCreatedData()
				if variant.name == "missing" {
					delete(data, field)
				} else {
					data[field] = variant.value
				}
				if got, err := Decode(checkpointCreatedFrame(t, data)); err == nil {
					t.Errorf("Decode accepted invalid checkpoint payload: %#v", got)
				}
			})
		}
	}

	for name, mutate := range map[string]func(map[string]any){
		"future-reason": func(data map[string]any) { data["reason"] = "saga.chapter_boundary" },
		"future-tool":   func(data map[string]any) { data["tool"] = "apply_patch" },
		"unicode-path":  func(data map[string]any) { data["path"] = "/work/kolkrabbi-🐙.md" },
	} {
		t.Run(name, func(t *testing.T) {
			data := validCheckpointCreatedData()
			mutate(data)
			if _, err := Decode(checkpointCreatedFrame(t, data)); err != nil {
				t.Fatalf("Decode restricted additive checkpoint context: %v", err)
			}
		})
	}
}

func TestCheckpointCreatedRequiresExplicitBooleanPrewriteState(t *testing.T) {
	for name, value := range map[string]any{
		"missing": nil,
		"null":    nil,
		"string":  "true",
		"number":  1,
	} {
		t.Run(name, func(t *testing.T) {
			data := validCheckpointCreatedData()
			if name == "missing" {
				delete(data, "existed")
			} else {
				data["existed"] = value
			}
			if got, err := Decode(checkpointCreatedFrame(t, data)); err == nil {
				t.Errorf("Decode accepted invalid pre-write state: %#v", got)
			}
		})
	}

	for _, existed := range []bool{false, true} {
		data := validCheckpointCreatedData()
		data["existed"] = existed
		if _, err := Decode(checkpointCreatedFrame(t, data)); err != nil {
			t.Fatalf("Decode rejected existed=%v: %v", existed, err)
		}
	}
}

func TestCheckpointCreatedTypedPayloadOrderAndUnknownFields(t *testing.T) {
	raw, err := json.Marshal(CheckpointCreatedData{
		ID: "checkpoint_000002", Reason: "before_write", Tool: "write_file",
		Path: "/work/new.txt", Existed: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"checkpoint_000002","reason":"before_write","tool":"write_file","path":"/work/new.txt","existed":false}`
	if string(raw) != want {
		t.Fatalf("typed payload = %s, want %s", raw, want)
	}
	if _, err := Decode(checkpointCreatedRawFrame(raw)); err != nil {
		t.Fatalf("typed payload does not satisfy its wire contract: %v", err)
	}

	data := validCheckpointCreatedData()
	data["future"] = "kept"
	got, err := Decode(checkpointCreatedFrame(t, data))
	if err != nil {
		t.Fatalf("Decode rejected additive field: %v", err)
	}
	if !bytes.Contains(got.Data, []byte(`"future":"kept"`)) {
		t.Errorf("unknown checkpoint field was not retained: %s", got.Data)
	}
}

func validCheckpointCreatedData() map[string]any {
	return map[string]any{
		"id": "checkpoint_000001", "reason": "before_write", "tool": "edit_file",
		"path": "/work/README.md", "existed": true,
	}
}

func checkpointCreatedFrame(t *testing.T, data map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return checkpointCreatedRawFrame(raw)
}

func checkpointCreatedRawFrame(raw []byte) []byte {
	return []byte(`{"seq":1,"ts":"2026-08-23T22:40:03Z","session":"` + goldenSession +
		`","turn":"` + goldenTurn + `","type":"checkpoint.created","data":` + string(raw) + `}`)
}

func assertCheckpointCreatedSchema(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "spec", "schemas", "events", "checkpoint.created.json"))
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
	wantID := "https://kolkrabbi.francomichetti.com/spec/0/schemas/events/checkpoint.created.json"
	if schema.Dialect != "https://json-schema.org/draft/2020-12/schema" || schema.ID != wantID ||
		schema.Title != "checkpoint.created payload" || schema.Type != "object" || !schema.AdditionalProperties {
		t.Error("schema root does not define the forward-compatible checkpoint.created payload")
	}
	wantRequired := []string{"id", "reason", "tool", "path", "existed"}
	if !reflect.DeepEqual(schema.Required, wantRequired) || len(schema.Properties) != len(wantRequired) {
		t.Errorf("schema fields = required %v, properties %v", schema.Required, schema.Properties)
	}
	for _, field := range []string{"id", "reason", "tool", "path"} {
		got := schema.Properties[field]
		if got.Type != "string" || got.MinLength == nil || *got.MinLength != 1 {
			t.Errorf("%s schema = %#v, want non-empty string", field, got)
		}
	}
	if got := schema.Properties["existed"]; got.Type != "boolean" || got.MinLength != nil {
		t.Errorf("existed schema = %#v, want required boolean", got)
	}
}
