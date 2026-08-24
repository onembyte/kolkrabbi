package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLogContractMatchesSchemaAndGolden(t *testing.T) {
	if string(EventLog) != "log" {
		t.Fatalf("event constant = %q, want schema and fixture name log", EventLog)
	}
	assertLogVocabularies(t)
	assertLogSchema(t)

	wantFrame, err := os.ReadFile(filepath.Join("..", "spec", "testdata", "events", "log.json"))
	if err != nil {
		t.Fatal(err)
	}
	wantFrame = bytes.TrimSpace(wantFrame)
	envelope, err := Decode(wantFrame)
	if err != nil {
		t.Fatalf("Decode(golden): %v", err)
	}
	if envelope.Type != EventLog {
		t.Errorf("golden type = %q, want %q", envelope.Type, EventLog)
	}
	var data LogData
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatal(err)
	}
	wantData := LogData{
		Level: LogLevelWarn, Code: LogCodeModelRotated, Field: "model",
		Was: "stealth/ox-alpha", Became: "qwen/qwen3-coder:free",
		Message: "rotated after a temporary provider limit",
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

func TestLogRequiresDefinedLevelAndCode(t *testing.T) {
	for _, level := range allLogLevels() {
		t.Run("level/"+string(level), func(t *testing.T) {
			data := validLogData()
			data["level"] = level
			if _, err := Decode(logFrame(t, data)); err != nil {
				t.Fatalf("Decode rejected level %q: %v", level, err)
			}
		})
	}
	for _, code := range allLogCodes() {
		t.Run("code/"+string(code), func(t *testing.T) {
			data := validLogData()
			data["code"] = code
			if _, err := Decode(logFrame(t, data)); err != nil {
				t.Fatalf("Decode rejected code %q: %v", code, err)
			}
		})
	}

	for name, mutate := range map[string]func(map[string]any){
		"level/missing":    func(data map[string]any) { delete(data, "level") },
		"level/null":       func(data map[string]any) { data["level"] = nil },
		"level/non-string": func(data map[string]any) { data["level"] = 1 },
		"level/error":      func(data map[string]any) { data["level"] = "error" },
		"code/missing":     func(data map[string]any) { delete(data, "code") },
		"code/null":        func(data map[string]any) { data["code"] = nil },
		"code/non-string":  func(data map[string]any) { data["code"] = 1 },
		"code/unknown":     func(data map[string]any) { data["code"] = "something_happened" },
	} {
		t.Run(name, func(t *testing.T) {
			data := validLogData()
			mutate(data)
			if got, err := Decode(logFrame(t, data)); err == nil {
				t.Errorf("Decode accepted invalid log vocabulary: %#v", got)
			}
		})
	}
}

func TestLogOptionalContextValidatesWhenPresent(t *testing.T) {
	for _, field := range []string{"field", "was", "became", "message"} {
		t.Run(field+"/omitted", func(t *testing.T) {
			data := validLogData()
			delete(data, field)
			if field == "field" {
				delete(data, "was")
				delete(data, "became")
			}
			if _, err := Decode(logFrame(t, data)); err != nil {
				t.Fatalf("Decode rejected omitted %s: %v", field, err)
			}
		})
		for name, value := range map[string]any{"empty": "", "null": nil, "non-string": 1} {
			t.Run(field+"/"+name, func(t *testing.T) {
				data := validLogData()
				data[field] = value
				if got, err := Decode(logFrame(t, data)); err == nil {
					t.Errorf("Decode accepted invalid optional %s: %#v", field, got)
				}
			})
		}
	}

	for _, transition := range []string{"was", "became"} {
		t.Run(transition+"/without-field", func(t *testing.T) {
			data := validLogData()
			delete(data, "field")
			if transition == "was" {
				delete(data, "became")
			} else {
				delete(data, "was")
			}
			if got, err := Decode(logFrame(t, data)); err == nil {
				t.Errorf("Decode accepted unowned %s transition: %#v", transition, got)
			}
		})
	}

	minimal := map[string]any{"level": "warn", "code": "usage_unavailable"}
	if _, err := Decode(logFrame(t, minimal)); err != nil {
		t.Fatalf("Decode rejected code-only diagnostic: %v", err)
	}
}

func TestLogTypedPayloadOrderAndUnknownFields(t *testing.T) {
	raw, err := json.Marshal(LogData{
		Level: LogLevelWarn, Code: LogCodeDeltasDropped, Field: "message.delta",
		Message: "3 delta frames dropped",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"level":"warn","code":"deltas_dropped","field":"message.delta","message":"3 delta frames dropped"}`
	if string(raw) != want {
		t.Fatalf("typed payload = %s, want %s", raw, want)
	}
	if _, err := Decode(logRawFrame(raw)); err != nil {
		t.Fatalf("typed payload does not satisfy its wire contract: %v", err)
	}

	data := validLogData()
	data["future"] = true
	got, err := Decode(logFrame(t, data))
	if err != nil {
		t.Fatalf("Decode rejected additive field: %v", err)
	}
	if !bytes.Contains(got.Data, []byte(`"future":true`)) {
		t.Errorf("unknown log field was not retained: %s", got.Data)
	}
}

func validLogData() map[string]any {
	return map[string]any{
		"level": "warn", "code": "model_rotated", "field": "model",
		"was": "stealth/ox-alpha", "became": "qwen/qwen3-coder:free",
		"message": "rotated after a temporary provider limit",
	}
}

func logFrame(t *testing.T, data map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return logRawFrame(raw)
}

func logRawFrame(raw []byte) []byte {
	return []byte(`{"seq":1,"ts":"2026-08-23T22:50:03Z","session":"` + goldenSession +
		`","turn":"` + goldenTurn + `","type":"log","data":` + string(raw) + `}`)
}

func allLogLevels() []LogLevel {
	return []LogLevel{LogLevelDebug, LogLevelInfo, LogLevelWarn}
}

func allLogCodes() []LogCode {
	return []LogCode{
		LogCodeToolsDropped, LogCodeToolsUnverified, LogCodeModelIgnored, LogCodeModelRotated,
		LogCodeEffortClamped, LogCodeEffortUnsupported, LogCodeCacheUnsupported,
		LogCodeHistoryTruncated, LogCodeHistoryLost, LogCodeFallbackIgnored,
		LogCodeUsageUnavailable, LogCodeCostUnavailable, LogCodeParamDropped,
		LogCodeToolCallTruncated, LogCodeToolIDRewritten, LogCodeDeltasDropped,
	}
}

func assertLogVocabularies(t *testing.T) {
	t.Helper()
	if got, want := allLogLevels(), []LogLevel{"debug", "info", "warn"}; !reflect.DeepEqual(got, want) {
		t.Errorf("log levels = %v, want %v", got, want)
	}
	wantCodes := []LogCode{
		"tools_dropped", "tools_unverified", "model_ignored", "model_rotated",
		"effort_clamped", "effort_unsupported", "cache_unsupported", "history_truncated",
		"history_lost", "fallback_ignored", "usage_unavailable", "cost_unavailable",
		"param_dropped", "tool_call_truncated", "tool_id_rewritten", "deltas_dropped",
	}
	if got := allLogCodes(); !reflect.DeepEqual(got, wantCodes) {
		t.Errorf("log codes = %v, want %v", got, wantCodes)
	}
}

func assertLogSchema(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "spec", "schemas", "events", "log.json"))
	if err != nil {
		t.Fatal(err)
	}
	type property struct {
		Type      string   `json:"type"`
		MinLength *int     `json:"minLength"`
		Enum      []string `json:"enum"`
	}
	var schema struct {
		Dialect              string              `json:"$schema"`
		ID                   string              `json:"$id"`
		Title                string              `json:"title"`
		Type                 string              `json:"type"`
		Required             []string            `json:"required"`
		Properties           map[string]property `json:"properties"`
		DependentRequired    map[string][]string `json:"dependentRequired"`
		AdditionalProperties bool                `json:"additionalProperties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("schema is not JSON: %v", err)
	}
	wantID := "https://kolkrabbi.francomichetti.com/spec/0/schemas/events/log.json"
	if schema.Dialect != "https://json-schema.org/draft/2020-12/schema" || schema.ID != wantID ||
		schema.Title != "log payload" || schema.Type != "object" || !schema.AdditionalProperties {
		t.Error("schema root does not define the forward-compatible log payload")
	}
	if !reflect.DeepEqual(schema.Required, []string{"level", "code"}) || len(schema.Properties) != 6 {
		t.Errorf("schema fields = required %v, properties %v", schema.Required, schema.Properties)
	}
	if got := schema.Properties["level"]; got.Type != "string" ||
		!reflect.DeepEqual(got.Enum, []string{"debug", "info", "warn"}) {
		t.Errorf("level schema = %#v", got)
	}
	wantCodes := make([]string, 0, len(allLogCodes()))
	for _, code := range allLogCodes() {
		wantCodes = append(wantCodes, string(code))
	}
	if got := schema.Properties["code"]; got.Type != "string" || !reflect.DeepEqual(got.Enum, wantCodes) {
		t.Errorf("code schema = %#v", got)
	}
	for _, field := range []string{"field", "was", "became", "message"} {
		got := schema.Properties[field]
		if got.Type != "string" || got.MinLength == nil || *got.MinLength != 1 {
			t.Errorf("%s schema = %#v, want optional non-empty string", field, got)
		}
	}
	wantDependencies := map[string][]string{"was": {"field"}, "became": {"field"}}
	if !reflect.DeepEqual(schema.DependentRequired, wantDependencies) {
		t.Errorf("dependentRequired = %v, want %v", schema.DependentRequired, wantDependencies)
	}
}
