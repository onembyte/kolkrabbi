package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestScoreRecordedContractMatchesSchemaAndGolden(t *testing.T) {
	if string(EventScoreRecorded) != "score.recorded" {
		t.Fatalf("event constant = %q, want schema and fixture name score.recorded", EventScoreRecorded)
	}
	assertScoreVocabularies(t)
	assertScoreRecordedSchema(t)

	wantFrame, err := os.ReadFile(filepath.Join("..", "spec", "testdata", "events", "score.recorded.json"))
	if err != nil {
		t.Fatal(err)
	}
	wantFrame = bytes.TrimSpace(wantFrame)
	envelope, err := Decode(wantFrame)
	if err != nil {
		t.Fatalf("Decode(golden): %v", err)
	}
	if envelope.Type != EventScoreRecorded {
		t.Errorf("golden type = %q, want %q", envelope.Type, EventScoreRecorded)
	}
	var data ScoreRecordedData
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatal(err)
	}
	wantData := ScoreRecordedData{
		ID: "score_rating_01", TargetKind: ScoreTargetTurn, TargetID: goldenTurn,
		Name: "rating", DataType: ScoreDataNumeric, Value: json.RawMessage("5"),
		Source: ScoreSourceHuman, Explanation: "helpful and correct",
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

func TestScoreRecordedRequiresIdentityTargetAndName(t *testing.T) {
	for _, field := range []string{"id", "target_id", "name"} {
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
				data := validScoreRecordedData()
				if variant.name == "missing" {
					delete(data, field)
				} else {
					data[field] = variant.value
				}
				if got, err := Decode(scoreRecordedFrame(t, data)); err == nil {
					t.Errorf("Decode accepted invalid score identity: %#v", got)
				}
			})
		}
	}

	validTargets := []struct {
		kind ScoreTargetKind
		id   string
	}{{ScoreTargetSession, goldenSession}, {ScoreTargetTurn, goldenTurn}, {ScoreTargetSpan, "span_attempt_01"}}
	for _, target := range validTargets {
		t.Run(string(target.kind), func(t *testing.T) {
			data := validScoreRecordedData()
			data["target_kind"], data["target_id"] = target.kind, target.id
			if _, err := Decode(scoreRecordedFrame(t, data)); err != nil {
				t.Fatalf("Decode rejected target %q: %v", target.kind, err)
			}
		})
	}

	for name, mutate := range map[string]func(map[string]any){
		"kind/missing":       func(data map[string]any) { delete(data, "target_kind") },
		"kind/null":          func(data map[string]any) { data["target_kind"] = nil },
		"kind/unknown":       func(data map[string]any) { data["target_kind"] = "message" },
		"session/wrong-kind": func(data map[string]any) { data["target_kind"], data["target_id"] = "session", goldenTurn },
		"turn/wrong-kind":    func(data map[string]any) { data["target_kind"], data["target_id"] = "turn", goldenSession },
		"span/empty":         func(data map[string]any) { data["target_kind"], data["target_id"] = "span", "" },
	} {
		t.Run(name, func(t *testing.T) {
			data := validScoreRecordedData()
			mutate(data)
			if got, err := Decode(scoreRecordedFrame(t, data)); err == nil {
				t.Errorf("Decode accepted invalid score target: %#v", got)
			}
		})
	}
}

func TestScoreRecordedValueMatchesDeclaredType(t *testing.T) {
	valid := []struct {
		name     string
		dataType ScoreDataType
		value    any
	}{
		{name: "numeric/integer", dataType: ScoreDataNumeric, value: 5},
		{name: "numeric/fraction", dataType: ScoreDataNumeric, value: -0.25},
		{name: "categorical", dataType: ScoreDataCategorical, value: "pass"},
		{name: "boolean/true", dataType: ScoreDataBoolean, value: true},
		{name: "boolean/false", dataType: ScoreDataBoolean, value: false},
		{name: "text", dataType: ScoreDataText, value: "tests passed"},
	}
	for _, tc := range valid {
		t.Run(tc.name, func(t *testing.T) {
			data := validScoreRecordedData()
			data["data_type"], data["value"] = tc.dataType, tc.value
			if _, err := Decode(scoreRecordedFrame(t, data)); err != nil {
				t.Fatalf("Decode rejected %s score: %v", tc.dataType, err)
			}
		})
	}

	invalid := []struct {
		name     string
		dataType any
		value    any
		missing  bool
	}{
		{name: "type/missing", value: 1},
		{name: "type/null", dataType: nil, value: 1},
		{name: "type/unknown", dataType: "percentage", value: 1},
		{name: "value/missing", dataType: "numeric", missing: true},
		{name: "value/null", dataType: "numeric", value: nil},
		{name: "value/object", dataType: "text", value: map[string]any{}},
		{name: "value/array", dataType: "categorical", value: []any{"pass"}},
		{name: "numeric/string", dataType: "numeric", value: "5"},
		{name: "categorical/number", dataType: "categorical", value: 1},
		{name: "categorical/empty", dataType: "categorical", value: ""},
		{name: "boolean/number", dataType: "boolean", value: 1},
		{name: "text/boolean", dataType: "text", value: true},
		{name: "text/empty", dataType: "text", value: ""},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			data := validScoreRecordedData()
			if tc.name == "type/missing" {
				delete(data, "data_type")
			} else {
				data["data_type"] = tc.dataType
			}
			if tc.missing {
				delete(data, "value")
			} else {
				data["value"] = tc.value
			}
			if got, err := Decode(scoreRecordedFrame(t, data)); err == nil {
				t.Errorf("Decode accepted invalid typed score: %#v", got)
			}
		})
	}
}

func TestScoreRecordedSourceOwnsJudgeModel(t *testing.T) {
	for _, source := range []ScoreSource{ScoreSourceHuman, ScoreSourceJudge, ScoreSourceImplicit} {
		t.Run(string(source), func(t *testing.T) {
			data := validScoreRecordedData()
			data["source"] = source
			if source == ScoreSourceJudge {
				data["judge_model"] = "openai/gpt-5-mini"
			}
			if _, err := Decode(scoreRecordedFrame(t, data)); err != nil {
				t.Fatalf("Decode rejected source %q: %v", source, err)
			}
		})
	}

	for name, mutate := range map[string]func(map[string]any){
		"source/missing": func(data map[string]any) { delete(data, "source") },
		"source/null":    func(data map[string]any) { data["source"] = nil },
		"source/unknown": func(data map[string]any) { data["source"] = "automatic" },
		"judge/model-missing": func(data map[string]any) {
			data["source"] = "judge"
			delete(data, "judge_model")
		},
		"judge/model-empty": func(data map[string]any) { data["source"], data["judge_model"] = "judge", "" },
		"judge/model-null":  func(data map[string]any) { data["source"], data["judge_model"] = "judge", nil },
		"judge/model-number": func(data map[string]any) {
			data["source"], data["judge_model"] = "judge", 1
		},
		"human/judge-model":    func(data map[string]any) { data["judge_model"] = "openai/gpt-5-mini" },
		"implicit/judge-model": func(data map[string]any) { data["source"], data["judge_model"] = "implicit", "local/judge" },
	} {
		t.Run(name, func(t *testing.T) {
			data := validScoreRecordedData()
			mutate(data)
			if got, err := Decode(scoreRecordedFrame(t, data)); err == nil {
				t.Errorf("Decode accepted invalid score provenance: %#v", got)
			}
		})
	}

	for name, value := range map[string]any{"empty": "", "null": nil, "non-string": 1} {
		t.Run("explanation/"+name, func(t *testing.T) {
			data := validScoreRecordedData()
			data["explanation"] = value
			if got, err := Decode(scoreRecordedFrame(t, data)); err == nil {
				t.Errorf("Decode accepted invalid explanation: %#v", got)
			}
		})
	}
	data := validScoreRecordedData()
	delete(data, "explanation")
	if _, err := Decode(scoreRecordedFrame(t, data)); err != nil {
		t.Fatalf("Decode rejected omitted explanation: %v", err)
	}
}

func TestScoreRecordedTypedPayloadOrderAndUnknownFields(t *testing.T) {
	raw, err := json.Marshal(ScoreRecordedData{
		ID: "score_judge_01", TargetKind: ScoreTargetSpan, TargetID: "span_attempt_01",
		Name: "correctness", DataType: ScoreDataCategorical, Value: json.RawMessage(`"pass"`),
		Source: ScoreSourceJudge, JudgeModel: "openai/gpt-5-mini", Explanation: "all checks passed",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"score_judge_01","target_kind":"span","target_id":"span_attempt_01","name":"correctness","data_type":"categorical","value":"pass","source":"judge","judge_model":"openai/gpt-5-mini","explanation":"all checks passed"}`
	if string(raw) != want {
		t.Fatalf("typed payload = %s, want %s", raw, want)
	}
	if _, err := Decode(scoreRecordedRawFrame(raw)); err != nil {
		t.Fatalf("typed payload does not satisfy its wire contract: %v", err)
	}

	data := validScoreRecordedData()
	data["future"] = true
	got, err := Decode(scoreRecordedFrame(t, data))
	if err != nil {
		t.Fatalf("Decode rejected additive field: %v", err)
	}
	if !bytes.Contains(got.Data, []byte(`"future":true`)) {
		t.Errorf("unknown score field was not retained: %s", got.Data)
	}
}

func validScoreRecordedData() map[string]any {
	return map[string]any{
		"id": "score_rating_01", "target_kind": "turn", "target_id": goldenTurn,
		"name": "rating", "data_type": "numeric", "value": 5, "source": "human",
		"explanation": "helpful and correct",
	}
}

func scoreRecordedFrame(t *testing.T, data map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return scoreRecordedRawFrame(raw)
}

func scoreRecordedRawFrame(raw []byte) []byte {
	return []byte(`{"seq":1,"ts":"2026-08-23T22:30:03Z","session":"` + goldenSession +
		`","turn":"` + goldenTurn + `","type":"score.recorded","data":` + string(raw) + `}`)
}

func assertScoreVocabularies(t *testing.T) {
	t.Helper()
	if got, want := []ScoreTargetKind{ScoreTargetSession, ScoreTargetTurn, ScoreTargetSpan},
		[]ScoreTargetKind{"session", "turn", "span"}; !reflect.DeepEqual(got, want) {
		t.Errorf("target kinds = %v, want %v", got, want)
	}
	if got, want := []ScoreDataType{ScoreDataNumeric, ScoreDataCategorical, ScoreDataBoolean, ScoreDataText},
		[]ScoreDataType{"numeric", "categorical", "boolean", "text"}; !reflect.DeepEqual(got, want) {
		t.Errorf("data types = %v, want %v", got, want)
	}
	if got, want := []ScoreSource{ScoreSourceHuman, ScoreSourceJudge, ScoreSourceImplicit},
		[]ScoreSource{"human", "judge", "implicit"}; !reflect.DeepEqual(got, want) {
		t.Errorf("sources = %v, want %v", got, want)
	}
}

func assertScoreRecordedSchema(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "spec", "schemas", "entities", "score.json"))
	if err != nil {
		t.Fatal(err)
	}
	type valueShape struct {
		Type      string `json:"type"`
		MinLength *int   `json:"minLength"`
	}
	type property struct {
		Type      string       `json:"type"`
		MinLength *int         `json:"minLength"`
		Enum      []string     `json:"enum"`
		OneOf     []valueShape `json:"oneOf"`
	}
	var schema struct {
		Dialect              string              `json:"$schema"`
		ID                   string              `json:"$id"`
		Title                string              `json:"title"`
		Type                 string              `json:"type"`
		Required             []string            `json:"required"`
		Properties           map[string]property `json:"properties"`
		AllOf                []json.RawMessage   `json:"allOf"`
		AdditionalProperties bool                `json:"additionalProperties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("schema is not JSON: %v", err)
	}
	wantID := "https://kolkrabbi.francomichetti.com/spec/0/schemas/entities/score.json"
	if schema.Dialect != "https://json-schema.org/draft/2020-12/schema" || schema.ID != wantID ||
		schema.Title != "score entity" || schema.Type != "object" || !schema.AdditionalProperties {
		t.Error("schema root does not define the forward-compatible score entity")
	}
	wantRequired := []string{"id", "target_kind", "target_id", "name", "data_type", "value", "source"}
	if !reflect.DeepEqual(schema.Required, wantRequired) || len(schema.Properties) != 9 {
		t.Errorf("schema fields = required %v, properties %v", schema.Required, schema.Properties)
	}
	for _, field := range []string{"id", "target_id", "name", "judge_model", "explanation"} {
		got := schema.Properties[field]
		if got.Type != "string" || got.MinLength == nil || *got.MinLength != 1 {
			t.Errorf("%s schema = %#v, want non-empty string", field, got)
		}
	}
	if got := schema.Properties["target_kind"]; got.Type != "string" ||
		!reflect.DeepEqual(got.Enum, []string{"session", "turn", "span"}) {
		t.Errorf("target_kind schema = %#v", got)
	}
	if got := schema.Properties["data_type"]; got.Type != "string" ||
		!reflect.DeepEqual(got.Enum, []string{"numeric", "categorical", "boolean", "text"}) {
		t.Errorf("data_type schema = %#v", got)
	}
	if got := schema.Properties["source"]; got.Type != "string" ||
		!reflect.DeepEqual(got.Enum, []string{"human", "judge", "implicit"}) {
		t.Errorf("source schema = %#v", got)
	}
	wantValueShapes := []valueShape{{Type: "number"}, {Type: "string", MinLength: intPointer(1)}, {Type: "boolean"}}
	if !reflect.DeepEqual(schema.Properties["value"].OneOf, wantValueShapes) {
		t.Errorf("value schema = %#v, want primitive union %#v", schema.Properties["value"], wantValueShapes)
	}
	if len(schema.AllOf) != 5 {
		t.Errorf("conditional schema clauses = %d, want four value types plus judge provenance", len(schema.AllOf))
	}
}

func intPointer(value int) *int { return &value }
