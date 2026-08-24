package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestErrorEventReusesSharedEntityContract(t *testing.T) {
	if string(EventError) != "error" {
		t.Fatalf("event constant = %q, want schema and fixture name error", EventError)
	}
	assertErrorEventSchemaReference(t)

	wantFrame, err := os.ReadFile(filepath.Join("..", "spec", "testdata", "events", "error.json"))
	if err != nil {
		t.Fatal(err)
	}
	wantFrame = bytes.TrimSpace(wantFrame)
	envelope, err := Decode(wantFrame)
	if err != nil {
		t.Fatalf("Decode(golden): %v", err)
	}
	if envelope.Type != EventError {
		t.Errorf("golden type = %q, want %q", envelope.Type, EventError)
	}

	wantEntity, err := os.ReadFile(filepath.Join("..", "spec", "testdata", "entities", "error.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(envelope.Data, bytes.TrimSpace(wantEntity)) {
		t.Errorf("event data does not reuse golden entity\n got: %s\nwant: %s", envelope.Data, bytes.TrimSpace(wantEntity))
	}
	var got Error
	if err := json.Unmarshal(envelope.Data, &got); err != nil {
		t.Fatal(err)
	}
	want := Error{
		Code: ErrorCodeRateLimited, Message: "provider capacity is temporarily limited",
		RetryAfterMilliseconds: int64Pointer(4000),
		Remedy:                 "retry later or select another model with /model",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("typed payload = %#v, want %#v", got, want)
	}
	roundTrip, err := Encode(envelope)
	if err != nil {
		t.Fatalf("Encode(golden): %v", err)
	}
	if !bytes.Equal(roundTrip, wantFrame) {
		t.Errorf("round trip drifted\n got: %s\nwant: %s", roundTrip, wantFrame)
	}
}

func TestErrorEventUsesSharedValidation(t *testing.T) {
	for _, code := range allErrorCodes() {
		t.Run(string(code), func(t *testing.T) {
			data := validErrorEntity()
			data["code"] = code
			if _, err := Decode(errorEventFrame(t, data)); err != nil {
				t.Fatalf("Decode rejected defined error code %q: %v", code, err)
			}
		})
	}

	for name, mutate := range map[string]func(map[string]any){
		"missing-code":  func(data map[string]any) { delete(data, "code") },
		"unknown-code":  func(data map[string]any) { data["code"] = "future_unregistered_failure" },
		"empty-message": func(data map[string]any) { data["message"] = "" },
		"null-delay":    func(data map[string]any) { data["retry_after_ms"] = nil },
		"empty-remedy":  func(data map[string]any) { data["remedy"] = "" },
	} {
		t.Run("invalid/"+name, func(t *testing.T) {
			data := validErrorEntity()
			mutate(data)
			if got, err := Decode(errorEventFrame(t, data)); err == nil {
				t.Errorf("Decode accepted malformed error entity: %#v", got)
			}
		})
	}
}

func TestErrorEventRetainsAdditiveEntityFields(t *testing.T) {
	data := validErrorEntity()
	data["future"] = map[string]any{"kept": true}
	got, err := Decode(errorEventFrame(t, data))
	if err != nil {
		t.Fatalf("Decode rejected additive entity field: %v", err)
	}
	if !bytes.Contains(got.Data, []byte(`"future":{"kept":true}`)) {
		t.Errorf("unknown error field was not retained: %s", got.Data)
	}
}

func errorEventFrame(t *testing.T, data map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return []byte(`{"seq":951,"ts":"2026-08-23T22:50:04Z","session":"` + goldenSession +
		`","turn":"` + goldenTurn + `","type":"error","data":` + string(raw) + `}`)
}

func assertErrorEventSchemaReference(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "spec", "schemas", "events", "error.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]json.RawMessage
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("schema is not JSON: %v", err)
	}
	if len(schema) != 4 {
		t.Fatalf("event schema fields = %v, want only dialect, id, title, and ref", schema)
	}
	for field, want := range map[string]string{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id":     "https://kolkrabbi.francomichetti.com/spec/0/schemas/events/error.json",
		"title":   "error payload",
		"$ref":    "../entities/error.json",
	} {
		var got string
		if err := json.Unmarshal(schema[field], &got); err != nil || got != want {
			t.Errorf("schema %s = %q (%v), want %q", field, got, err, want)
		}
	}
	for _, duplicated := range []string{"type", "required", "properties", "additionalProperties"} {
		if _, present := schema[duplicated]; present {
			t.Errorf("event schema restates entity field %q", duplicated)
		}
	}
}
