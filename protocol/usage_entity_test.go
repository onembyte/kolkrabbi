package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestUsageEntityIsTheUsageReportedPayload(t *testing.T) {
	assertUsageEventSchemaReference(t)

	wantEntity, err := os.ReadFile(filepath.Join("..", "spec", "testdata", "entities", "usage.json"))
	if err != nil {
		t.Fatal(err)
	}
	wantEntity = bytes.TrimSpace(wantEntity)
	if err := validateUsageEntity(wantEntity); err != nil {
		t.Fatalf("validateUsageEntity(golden): %v", err)
	}

	var got Usage
	if err := json.Unmarshal(wantEntity, &got); err != nil {
		t.Fatal(err)
	}
	want := Usage{
		Model: "anthropic/claude-sonnet-4", ProviderName: "Anthropic",
		RequestModel: "anthropic/claude-sonnet-4", ResponseModel: "anthropic/claude-sonnet-4",
		InputTokens: int64Pointer(1200), CacheReadTokens: int64Pointer(800),
		CacheWriteTokens: int64Pointer(0), OutputTokens: int64Pointer(240),
		ReasoningTokens: int64Pointer(64), CostUSD: float64Pointer(0.0123),
		CostSource: UsageCostReported, Measurement: UsageMeasurementMetered,
		TTFTMilliseconds: int64Pointer(820), FinishReason: "stop", GenID: "gen_abc123",
		Attempt: 1, Role: "main", Effort: "standard",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("typed entity = %#v, want %#v", got, want)
	}
	if reflect.TypeOf(UsageReportedData{}) != reflect.TypeOf(Usage{}) {
		t.Error("UsageReportedData is a second named type instead of an alias of Usage")
	}
	roundTrip, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(roundTrip, wantEntity) {
		t.Errorf("entity round trip drifted\n got: %s\nwant: %s", roundTrip, wantEntity)
	}

	eventFrame, err := os.ReadFile(filepath.Join("..", "spec", "testdata", "events", "usage.reported.json"))
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := Decode(bytes.TrimSpace(eventFrame))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(envelope.Data, wantEntity) {
		t.Errorf("event data does not reuse golden entity\n got: %s\nwant: %s", envelope.Data, wantEntity)
	}
}

func assertUsageEventSchemaReference(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "spec", "schemas", "events", "usage.reported.json"))
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
		"$id":     "https://kolkrabbi.francomichetti.com/spec/0/schemas/events/usage.reported.json",
		"title":   "usage.reported payload",
		"$ref":    "../entities/usage.json",
	} {
		var got string
		if err := json.Unmarshal(schema[field], &got); err != nil || got != want {
			t.Errorf("schema %s = %q (%v), want %q", field, got, err, want)
		}
	}
}
