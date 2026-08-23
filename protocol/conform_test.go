package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

const (
	goldenSession = "s_01ARYZ6S41TSV4RRFFQ69G5FAV"
	goldenTurn    = "t_01ARYZ6S41TSV4RRFFQ69G5FAW"
)

func TestVersionMirrorsLanguageNeutralSpec(t *testing.T) {
	raw, err := os.ReadFile("../spec/VERSION")
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "0\n" {
		t.Fatalf("spec/VERSION = %q, want exactly %q", raw, "0\\n")
	}
	if Version != strings.TrimSpace(string(raw)) {
		t.Errorf("protocol.Version = %q, spec/VERSION = %q", Version, strings.TrimSpace(string(raw)))
	}
}

func TestEnvelopeSchemaDeclaresTheWireContract(t *testing.T) {
	raw, err := os.ReadFile("../spec/schemas/envelope.json")
	if err != nil {
		t.Fatal(err)
	}
	type property struct {
		Type    string `json:"type"`
		Minimum int    `json:"minimum"`
		Format  string `json:"format"`
		Pattern string `json:"pattern"`
	}
	var schema struct {
		Dialect              string              `json:"$schema"`
		ID                   string              `json:"$id"`
		Type                 string              `json:"type"`
		Required             []string            `json:"required"`
		Properties           map[string]property `json:"properties"`
		AdditionalProperties bool                `json:"additionalProperties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("schema is not JSON: %v", err)
	}
	if schema.Dialect != "https://json-schema.org/draft/2020-12/schema" {
		t.Errorf("schema dialect = %q", schema.Dialect)
	}
	if schema.ID != "https://kolkrabbi.francomichetti.com/spec/0/schemas/envelope.json" {
		t.Errorf("schema id = %q", schema.ID)
	}
	if schema.Type != "object" || !schema.AdditionalProperties {
		t.Errorf("envelope must be a forward-compatible object")
	}
	want := []string{"seq", "ts", "session", "turn", "type", "data"}
	if !reflect.DeepEqual(schema.Required, want) {
		t.Errorf("required = %v, want %v in wire order", schema.Required, want)
	}
	for _, name := range want {
		if schema.Properties[name].Type == "" {
			t.Errorf("schema has no property %q", name)
		}
	}
	if got := schema.Properties["seq"]; got.Type != "integer" || got.Minimum != 1 {
		t.Errorf("seq schema = %#v", got)
	}
	if got := schema.Properties["ts"]; got.Type != "string" || got.Format != "date-time" {
		t.Errorf("ts schema = %#v", got)
	}
	if got := schema.Properties["data"]; got.Type != "object" {
		t.Errorf("data schema = %#v", got)
	}

	patternCases := []struct {
		name    string
		pattern string
		valid   string
		invalid string
	}{
		{"session", schema.Properties["session"].Pattern, goldenSession, goldenTurn},
		{"turn", schema.Properties["turn"].Pattern, goldenTurn, goldenSession},
		{"type", schema.Properties["type"].Pattern, "message.delta", "Message delta"},
	}
	for _, tc := range patternCases {
		t.Run(tc.name+" pattern", func(t *testing.T) {
			re, err := regexp.Compile(tc.pattern)
			if err != nil {
				t.Fatalf("invalid schema pattern %q: %v", tc.pattern, err)
			}
			if !re.MatchString(tc.valid) || re.MatchString(tc.invalid) {
				t.Errorf("pattern %q did not distinguish %q from %q", tc.pattern, tc.valid, tc.invalid)
			}
		})
	}
}

func TestGoldenEnvelopeRoundTripsByteForByte(t *testing.T) {
	want, err := os.ReadFile("../spec/testdata/events/message.delta.json")
	if err != nil {
		t.Fatal(err)
	}
	want = bytes.TrimSpace(want)

	got, err := Decode(want)
	if err != nil {
		t.Fatalf("Decode(golden): %v", err)
	}
	if got.Seq != 412 || !got.Timestamp.Equal(time.Date(2026, 8, 23, 18, 30, 12, 345_000_000, time.UTC)) ||
		got.Session != goldenSession || got.Turn != goldenTurn || got.Type != "message.delta" {
		t.Errorf("decoded envelope = %#v", got)
	}
	var data struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(got.Data, &data); err != nil || data.Text != "hello from kolk" {
		t.Errorf("decoded data = %q, err = %v", got.Data, err)
	}

	encoded, err := Encode(got)
	if err != nil {
		t.Fatalf("Encode(golden): %v", err)
	}
	if !bytes.Equal(encoded, want) {
		t.Errorf("round trip drifted\n got: %s\nwant: %s", encoded, want)
	}
}

func TestDecodeIgnoresUnknownFieldsAndEventTypes(t *testing.T) {
	raw := []byte(`{"seq":1,"ts":"2026-08-23T18:30:12Z","session":"` + goldenSession + `","turn":"` + goldenTurn + `","type":"future.shipped","data":{"known":true,"future":"kept"},"future_top":"ignored"}`)
	got, err := Decode(raw)
	if err != nil {
		t.Fatalf("forward-compatible Decode: %v", err)
	}
	if got.Type != "future.shipped" || !bytes.Contains(got.Data, []byte(`"future":"kept"`)) {
		t.Errorf("unknown type or payload fields were lost: %#v", got)
	}
}

func TestEnvelopeRejectsInvalidWireValues(t *testing.T) {
	valid := `{"seq":1,"ts":"2026-08-23T18:30:12Z","session":"` + goldenSession + `","turn":"` + goldenTurn + `","type":"message.delta","data":{}}`
	tests := map[string]string{
		"missing sequence":   strings.Replace(valid, `"seq":1,`, ``, 1),
		"zero sequence":      strings.Replace(valid, `"seq":1`, `"seq":0`, 1),
		"missing timestamp":  strings.Replace(valid, `"ts":"2026-08-23T18:30:12Z",`, ``, 1),
		"bad timestamp":      strings.Replace(valid, `2026-08-23T18:30:12Z`, `yesterday`, 1),
		"missing session":    strings.Replace(valid, `"session":"`+goldenSession+`",`, ``, 1),
		"wrong session kind": strings.Replace(valid, goldenSession, "t_01ARYZ6S41TSV4RRFFQ69G5FAV", 1),
		"lowercase session":  strings.Replace(valid, goldenSession, strings.ToLower(goldenSession), 1),
		"overflow session":   strings.Replace(valid, goldenSession, "s_81ARYZ6S41TSV4RRFFQ69G5FAV", 1),
		"forbidden ID rune":  strings.Replace(valid, goldenSession, "s_01ARYZ6S41TSV4RRFFQ69G5FAU", 1),
		"missing turn":       strings.Replace(valid, `"turn":"`+goldenTurn+`",`, ``, 1),
		"wrong turn kind":    strings.Replace(valid, goldenTurn, "s_01ARYZ6S41TSV4RRFFQ69G5FAW", 1),
		"missing event type": strings.Replace(valid, `"type":"message.delta",`, ``, 1),
		"bad event type":     strings.Replace(valid, `message.delta`, `Message delta`, 1),
		"empty type segment": strings.Replace(valid, `message.delta`, `message..delta`, 1),
		"missing data":       strings.Replace(valid, `,"data":{}`, ``, 1),
		"null data":          strings.Replace(valid, `"data":{}`, `"data":null`, 1),
		"array data":         strings.Replace(valid, `"data":{}`, `"data":[]`, 1),
		"trailing value":     valid + `{}`,
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			if got, err := Decode([]byte(raw)); err == nil {
				t.Errorf("Decode accepted invalid frame: %#v", got)
			}
		})
	}

	if got, err := Encode(Envelope{}); err == nil {
		t.Errorf("Encode accepted a zero envelope: %s", got)
	}
}
