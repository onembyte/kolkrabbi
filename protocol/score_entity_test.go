package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestScoreEntityIsTheScoreRecordedPayload(t *testing.T) {
	assertScoreEventSchemaReference(t)

	wantEntity, err := os.ReadFile(filepath.Join("..", "spec", "testdata", "entities", "score.json"))
	if err != nil {
		t.Fatal(err)
	}
	wantEntity = bytes.TrimSpace(wantEntity)
	if err := validateScoreEntity(wantEntity); err != nil {
		t.Fatalf("validateScoreEntity(golden): %v", err)
	}

	var got Score
	if err := json.Unmarshal(wantEntity, &got); err != nil {
		t.Fatal(err)
	}
	want := Score{
		ID: "score_rating_01", TargetKind: ScoreTargetTurn, TargetID: goldenTurn,
		Name: "rating", DataType: ScoreDataNumeric, Value: json.RawMessage("5"),
		Source: ScoreSourceHuman, Explanation: "helpful and correct",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("typed entity = %#v, want %#v", got, want)
	}
	if reflect.TypeOf(ScoreRecordedData{}) != reflect.TypeOf(Score{}) {
		t.Error("ScoreRecordedData is a second named type instead of an alias of Score")
	}
	roundTrip, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(roundTrip, wantEntity) {
		t.Errorf("entity round trip drifted\n got: %s\nwant: %s", roundTrip, wantEntity)
	}

	eventFrame, err := os.ReadFile(filepath.Join("..", "spec", "testdata", "events", "score.recorded.json"))
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

func assertScoreEventSchemaReference(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "spec", "schemas", "events", "score.recorded.json"))
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
		"$id":     "https://kolkrabbi.francomichetti.com/spec/0/schemas/events/score.recorded.json",
		"title":   "score.recorded payload",
		"$ref":    "../entities/score.json",
	} {
		var got string
		if err := json.Unmarshal(schema[field], &got); err != nil || got != want {
			t.Errorf("schema %s = %q (%v), want %q", field, got, err, want)
		}
	}
}
