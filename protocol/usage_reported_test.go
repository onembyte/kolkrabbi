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

func TestUsageReportedContractMatchesSchemaGoldenAndMapping(t *testing.T) {
	if string(EventUsageReported) != "usage.reported" {
		t.Fatalf("event constant = %q, want schema and fixture name usage.reported", EventUsageReported)
	}
	assertUsageReportedVocabularies(t)
	assertUsageReportedSchema(t)
	assertUsageReportedMapping(t)

	wantFrame, err := os.ReadFile(filepath.Join("..", "spec", "testdata", "events", "usage.reported.json"))
	if err != nil {
		t.Fatal(err)
	}
	wantFrame = bytes.TrimSpace(wantFrame)
	envelope, err := Decode(wantFrame)
	if err != nil {
		t.Fatalf("Decode(golden): %v", err)
	}
	if envelope.Type != EventUsageReported {
		t.Errorf("golden type = %q, want %q", envelope.Type, EventUsageReported)
	}
	var data UsageReportedData
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatal(err)
	}
	wantData := UsageReportedData{
		Model: "anthropic/claude-sonnet-4", ProviderName: "Anthropic",
		RequestModel: "anthropic/claude-sonnet-4", ResponseModel: "anthropic/claude-sonnet-4",
		InputTokens: int64Pointer(1200), CacheReadTokens: int64Pointer(800),
		CacheWriteTokens: int64Pointer(0), OutputTokens: int64Pointer(240),
		ReasoningTokens: int64Pointer(64), CostUSD: float64Pointer(0.0123),
		CostSource: UsageCostReported, Measurement: UsageMeasurementMetered,
		TTFTMilliseconds: int64Pointer(820), FinishReason: "stop", GenID: "gen_abc123",
		Attempt: 1, Role: "main", Effort: "standard",
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

func TestUsageReportedRequiresAttemptIdentityAndContext(t *testing.T) {
	for _, field := range []string{"model", "provider_name", "request_model", "role", "effort"} {
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
				data := validUsageReportedData()
				if variant.name == "missing" {
					delete(data, field)
				} else {
					data[field] = variant.value
				}
				if got, err := Decode(usageReportedFrame(t, data)); err == nil {
					t.Errorf("Decode accepted invalid usage identity/context: %#v", got)
				}
			})
		}
	}

	for name, value := range map[string]any{
		"missing":    nil,
		"null":       nil,
		"zero":       0,
		"negative":   -1,
		"fractional": 1.5,
		"string":     "1",
	} {
		t.Run("attempt/"+name, func(t *testing.T) {
			data := validUsageReportedData()
			if name == "missing" {
				delete(data, "attempt")
			} else {
				data["attempt"] = value
			}
			if got, err := Decode(usageReportedFrame(t, data)); err == nil {
				t.Errorf("Decode accepted invalid usage attempt: %#v", got)
			}
		})
	}
}

func TestUsageReportedOptionalMeasurementsDistinguishUnknownAndZero(t *testing.T) {
	for _, field := range []string{
		"input_tokens", "cache_read_tokens", "cache_write_tokens", "output_tokens", "reasoning_tokens", "ttft_ms",
	} {
		t.Run(field+"/omitted", func(t *testing.T) {
			data := validUsageReportedData()
			delete(data, field)
			if _, err := Decode(usageReportedFrame(t, data)); err != nil {
				t.Fatalf("Decode rejected unknown %s: %v", field, err)
			}
		})
		t.Run(field+"/zero", func(t *testing.T) {
			data := validUsageReportedData()
			data[field] = 0
			if _, err := Decode(usageReportedFrame(t, data)); err != nil {
				t.Fatalf("Decode rejected measured zero %s: %v", field, err)
			}
		})
		for name, value := range map[string]any{
			"null": nil, "negative": -1, "fractional": 1.5, "string": "0",
		} {
			t.Run(field+"/"+name, func(t *testing.T) {
				data := validUsageReportedData()
				data[field] = value
				if got, err := Decode(usageReportedFrame(t, data)); err == nil {
					t.Errorf("Decode accepted invalid %s: %#v", field, got)
				}
			})
		}
	}
}

func TestUsageReportedCostSourcePreservesUnknownFreeAndMeasured(t *testing.T) {
	sources := []UsageCostSource{
		UsageCostUnknown, UsageCostReported, UsageCostHeader, UsageCostFollowup,
		UsageCostPriceTable, UsageCostVendorEstimate, UsageCostFree,
	}
	for _, source := range sources {
		t.Run(string(source), func(t *testing.T) {
			data := validUsageReportedData()
			data["cost_source"] = source
			switch source {
			case UsageCostUnknown:
				delete(data, "cost_usd")
			case UsageCostFree:
				data["cost_usd"] = 0
			default:
				data["cost_usd"] = 0.0123
			}
			if _, err := Decode(usageReportedFrame(t, data)); err != nil {
				t.Fatalf("Decode rejected cost source %q: %v", source, err)
			}
		})
	}

	invalid := map[string]func(map[string]any){
		"source/missing":       func(data map[string]any) { delete(data, "cost_source") },
		"source/null":          func(data map[string]any) { data["cost_source"] = nil },
		"source/unknown-value": func(data map[string]any) { data["cost_source"] = "estimated_by_magic" },
		"cost/null":            func(data map[string]any) { data["cost_usd"] = nil },
		"cost/negative":        func(data map[string]any) { data["cost_usd"] = -0.01 },
		"cost/string":          func(data map[string]any) { data["cost_usd"] = "0.01" },
		"unknown/with-cost": func(data map[string]any) {
			data["cost_source"], data["cost_usd"] = UsageCostUnknown, 0
		},
		"free/missing-cost": func(data map[string]any) {
			data["cost_source"] = UsageCostFree
			delete(data, "cost_usd")
		},
		"free/nonzero-cost": func(data map[string]any) {
			data["cost_source"], data["cost_usd"] = UsageCostFree, 0.01
		},
		"measured/missing-cost": func(data map[string]any) { delete(data, "cost_usd") },
	}
	for name, mutate := range invalid {
		t.Run(name, func(t *testing.T) {
			data := validUsageReportedData()
			mutate(data)
			if got, err := Decode(usageReportedFrame(t, data)); err == nil {
				t.Errorf("Decode accepted invalid cost semantics: %#v", got)
			}
		})
	}
}

func TestUsageReportedMeasurementVocabulary(t *testing.T) {
	for _, measurement := range []UsageMeasurement{
		UsageMeasurementUnknown, UsageMeasurementMetered, UsageMeasurementEstimated, UsageMeasurementLocal,
	} {
		t.Run(string(measurement), func(t *testing.T) {
			data := validUsageReportedData()
			data["measurement"] = measurement
			if _, err := Decode(usageReportedFrame(t, data)); err != nil {
				t.Fatalf("Decode rejected measurement %q: %v", measurement, err)
			}
		})
	}
	for name, value := range map[string]any{"missing": nil, "null": nil, "unknown": "precise"} {
		t.Run("invalid/"+name, func(t *testing.T) {
			data := validUsageReportedData()
			if name == "missing" {
				delete(data, "measurement")
			} else {
				data["measurement"] = value
			}
			if got, err := Decode(usageReportedFrame(t, data)); err == nil {
				t.Errorf("Decode accepted invalid measurement: %#v", got)
			}
		})
	}
}

func TestUsageReportedOptionalStringsValidateWhenPresent(t *testing.T) {
	for _, field := range []string{"response_model", "finish_reason", "error_type", "gen_id"} {
		t.Run(field+"/omitted", func(t *testing.T) {
			data := validUsageReportedData()
			delete(data, field)
			if _, err := Decode(usageReportedFrame(t, data)); err != nil {
				t.Fatalf("Decode rejected omitted %s: %v", field, err)
			}
		})
		for name, value := range map[string]any{"empty": "", "null": nil, "non-string": 1} {
			t.Run(field+"/"+name, func(t *testing.T) {
				data := validUsageReportedData()
				data[field] = value
				if got, err := Decode(usageReportedFrame(t, data)); err == nil {
					t.Errorf("Decode accepted invalid optional %s: %#v", field, got)
				}
			})
		}
	}
}

func TestUsageReportedTypedPayloadOrderAndUnknownFields(t *testing.T) {
	raw, err := json.Marshal(UsageReportedData{
		Model: "local/test", ProviderName: "local", RequestModel: "local/test",
		InputTokens: int64Pointer(0), CostUSD: float64Pointer(0), CostSource: UsageCostFree,
		Measurement: UsageMeasurementLocal, TTFTMilliseconds: int64Pointer(0),
		Attempt: 2, Role: "subagent", Effort: "quick",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"model":"local/test","provider_name":"local","request_model":"local/test","input_tokens":0,"cost_usd":0,"cost_source":"free","measurement":"local","ttft_ms":0,"attempt":2,"role":"subagent","effort":"quick"}`
	if string(raw) != want {
		t.Fatalf("typed payload = %s, want %s", raw, want)
	}
	if _, err := Decode(usageReportedRawFrame(raw)); err != nil {
		t.Fatalf("typed payload does not satisfy its wire contract: %v", err)
	}

	data := validUsageReportedData()
	data["future"] = map[string]any{"kept": true}
	got, err := Decode(usageReportedFrame(t, data))
	if err != nil {
		t.Fatalf("Decode rejected additive field: %v", err)
	}
	if !bytes.Contains(got.Data, []byte(`"future":{"kept":true}`)) {
		t.Errorf("unknown usage field was not retained: %s", got.Data)
	}
}

func validUsageReportedData() map[string]any {
	return map[string]any{
		"model": "anthropic/claude-sonnet-4", "provider_name": "Anthropic",
		"request_model": "anthropic/claude-sonnet-4", "response_model": "anthropic/claude-sonnet-4",
		"input_tokens": 1200, "cache_read_tokens": 800, "cache_write_tokens": 0,
		"output_tokens": 240, "reasoning_tokens": 64, "cost_usd": 0.0123,
		"cost_source": "reported", "measurement": "metered", "ttft_ms": 820,
		"finish_reason": "stop", "gen_id": "gen_abc123", "attempt": 1,
		"role": "main", "effort": "standard",
	}
}

func usageReportedFrame(t *testing.T, data map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return usageReportedRawFrame(raw)
}

func usageReportedRawFrame(raw []byte) []byte {
	return []byte(`{"seq":1,"ts":"2026-08-23T22:15:03Z","session":"` + goldenSession +
		`","turn":"` + goldenTurn + `","type":"usage.reported","data":` + string(raw) + `}`)
}

func int64Pointer(value int64) *int64       { return &value }
func float64Pointer(value float64) *float64 { return &value }

func assertUsageReportedVocabularies(t *testing.T) {
	t.Helper()
	wantCosts := []UsageCostSource{
		"unknown", "reported", "header", "followup", "price_table", "vendor_estimate", "free",
	}
	gotCosts := []UsageCostSource{
		UsageCostUnknown, UsageCostReported, UsageCostHeader, UsageCostFollowup,
		UsageCostPriceTable, UsageCostVendorEstimate, UsageCostFree,
	}
	if !reflect.DeepEqual(gotCosts, wantCosts) {
		t.Fatalf("cost sources = %v, want %v", gotCosts, wantCosts)
	}
	wantMeasurements := []UsageMeasurement{"unknown", "metered", "estimated", "local"}
	gotMeasurements := []UsageMeasurement{
		UsageMeasurementUnknown, UsageMeasurementMetered, UsageMeasurementEstimated, UsageMeasurementLocal,
	}
	if !reflect.DeepEqual(gotMeasurements, wantMeasurements) {
		t.Fatalf("measurements = %v, want %v", gotMeasurements, wantMeasurements)
	}
}

func assertUsageReportedMapping(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "spec", "provider-usage.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, field := range usageReportedFieldNames() {
		if !strings.Contains(text, "| `"+field+"` |") {
			t.Errorf("provider usage mapping does not name %s", field)
		}
	}
	for _, statement := range []string{
		"omitted means unknown", "explicit zero is", "`cost_source: unknown`", "`cost_source: free`",
	} {
		if !strings.Contains(text, statement) {
			t.Errorf("provider usage mapping lacks semantic statement %q", statement)
		}
	}
}

func assertUsageReportedSchema(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "spec", "schemas", "entities", "usage.json"))
	if err != nil {
		t.Fatal(err)
	}
	type property struct {
		Type      string   `json:"type"`
		MinLength *int     `json:"minLength"`
		Minimum   *float64 `json:"minimum"`
		Enum      []string `json:"enum"`
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
	wantID := "https://kolkrabbi.francomichetti.com/spec/0/schemas/entities/usage.json"
	if schema.Dialect != "https://json-schema.org/draft/2020-12/schema" || schema.ID != wantID ||
		schema.Title != "usage entity" || schema.Type != "object" || !schema.AdditionalProperties {
		t.Errorf("schema root does not define the forward-compatible usage entity")
	}
	wantRequired := []string{
		"model", "provider_name", "request_model", "cost_source", "measurement", "attempt", "role", "effort",
	}
	if !reflect.DeepEqual(schema.Required, wantRequired) {
		t.Errorf("required = %v, want %v", schema.Required, wantRequired)
	}
	wantFields := usageReportedFieldNames()
	if len(schema.Properties) != len(wantFields) {
		t.Fatalf("schema properties = %d, want exactly %d", len(schema.Properties), len(wantFields))
	}
	for _, field := range wantFields {
		if _, ok := schema.Properties[field]; !ok {
			t.Errorf("schema is missing %s", field)
		}
	}
	for _, field := range []string{
		"model", "provider_name", "request_model", "response_model", "finish_reason", "error_type", "gen_id", "role", "effort",
	} {
		got := schema.Properties[field]
		if got.Type != "string" || got.MinLength == nil || *got.MinLength != 1 {
			t.Errorf("%s schema = %#v, want non-empty string", field, got)
		}
	}
	for _, field := range []string{
		"input_tokens", "cache_read_tokens", "cache_write_tokens", "output_tokens", "reasoning_tokens", "ttft_ms",
	} {
		got := schema.Properties[field]
		if got.Type != "integer" || got.Minimum == nil || *got.Minimum != 0 {
			t.Errorf("%s schema = %#v, want integer >= 0", field, got)
		}
	}
	if got := schema.Properties["attempt"]; got.Type != "integer" || got.Minimum == nil || *got.Minimum != 1 {
		t.Errorf("attempt schema = %#v, want integer >= 1", got)
	}
	if got := schema.Properties["cost_usd"]; got.Type != "number" || got.Minimum == nil || *got.Minimum != 0 {
		t.Errorf("cost_usd schema = %#v, want number >= 0", got)
	}
	wantCostEnum := []string{"unknown", "reported", "header", "followup", "price_table", "vendor_estimate", "free"}
	if got := schema.Properties["cost_source"]; got.Type != "string" || !reflect.DeepEqual(got.Enum, wantCostEnum) {
		t.Errorf("cost_source schema = %#v", got)
	}
	wantMeasurementEnum := []string{"unknown", "metered", "estimated", "local"}
	if got := schema.Properties["measurement"]; got.Type != "string" || !reflect.DeepEqual(got.Enum, wantMeasurementEnum) {
		t.Errorf("measurement schema = %#v", got)
	}
}

func usageReportedFieldNames() []string {
	return []string{
		"model", "provider_name", "request_model", "response_model", "input_tokens",
		"cache_read_tokens", "cache_write_tokens", "output_tokens", "reasoning_tokens", "cost_usd",
		"cost_source", "measurement", "ttft_ms", "finish_reason", "error_type", "gen_id",
		"attempt", "role", "effort",
	}
}
