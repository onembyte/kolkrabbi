package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestHelloContractMatchesSchemaAndGolden(t *testing.T) {
	if string(EventHello) != "hello" {
		t.Fatalf("event constant = %q, want schema and fixture name %q", EventHello, "hello")
	}
	assertHelloSchema(t)

	want, err := os.ReadFile("../spec/testdata/events/hello.json")
	if err != nil {
		t.Fatal(err)
	}
	want = bytes.TrimSpace(want)
	envelope, err := Decode(want)
	if err != nil {
		t.Fatalf("Decode(golden): %v", err)
	}
	if envelope.Type != EventHello {
		t.Errorf("golden type = %q, want %q", envelope.Type, EventHello)
	}
	var data HelloData
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatalf("typed payload: %v", err)
	}
	if data.Protocol != Version || data.Server != "kolk 0.1.0" ||
		!reflect.DeepEqual(data.Capabilities, []string{"shell:posix"}) {
		t.Errorf("hello data = %#v", data)
	}
	got, err := Encode(envelope)
	if err != nil {
		t.Fatalf("Encode(golden): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("round trip drifted\n got: %s\nwant: %s", got, want)
	}
}

func TestHelloRejectsInvalidBaselineFields(t *testing.T) {
	valid := `{"protocol":"0","server":"kolk 0.1.0","capabilities":["shell:posix"]}`
	tests := map[string]string{
		"missing protocol":       `{"server":"kolk 0.1.0","capabilities":["shell:posix"]}`,
		"wrong protocol":         `{"protocol":"1","server":"kolk 0.1.0","capabilities":["shell:posix"]}`,
		"missing server":         `{"protocol":"0","capabilities":["shell:posix"]}`,
		"empty server":           `{"protocol":"0","server":"","capabilities":["shell:posix"]}`,
		"missing capabilities":   `{"protocol":"0","server":"kolk 0.1.0"}`,
		"null capabilities":      `{"protocol":"0","server":"kolk 0.1.0","capabilities":null}`,
		"non-array capabilities": `{"protocol":"0","server":"kolk 0.1.0","capabilities":{}}`,
		"empty capability":       `{"protocol":"0","server":"kolk 0.1.0","capabilities":[""]}`,
		"duplicate capability":   `{"protocol":"0","server":"kolk 0.1.0","capabilities":["shell:posix","shell:posix"]}`,
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			if got, err := Decode(helloFrame(data)); err == nil {
				t.Errorf("Decode accepted invalid hello payload: %#v", got)
			}
		})
	}

	t.Run("empty capability list", func(t *testing.T) {
		data := `{"protocol":"0","server":"kolk 0.1.0","capabilities":[],"future":true}`
		got, err := Decode(helloFrame(data))
		if err != nil {
			t.Fatalf("Decode rejected an honest empty capability list: %v", err)
		}
		if !bytes.Contains(got.Data, []byte(`"future":true`)) {
			t.Errorf("unknown hello field was not retained: %s", got.Data)
		}
	})

	if _, err := Decode(helloFrame(valid)); err != nil {
		t.Fatalf("valid hello baseline: %v", err)
	}
}

func helloFrame(data string) []byte {
	return []byte(`{"seq":1,"ts":"2026-08-23T18:30:12Z","session":"` + goldenSession +
		`","turn":"` + goldenTurn + `","type":"hello","data":` + data + `}`)
}

func assertHelloSchema(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile("../spec/schemas/events/hello.json")
	if err != nil {
		t.Fatal(err)
	}
	type property struct {
		Type        string `json:"type"`
		Const       string `json:"const"`
		MinLength   int    `json:"minLength"`
		UniqueItems bool   `json:"uniqueItems"`
		Items       *property
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
	if schema.Dialect != "https://json-schema.org/draft/2020-12/schema" ||
		schema.ID != "https://kolkrabbi.francomichetti.com/spec/0/schemas/events/hello.json" {
		t.Errorf("schema identity = (%q, %q)", schema.Dialect, schema.ID)
	}
	if schema.Title != "hello payload" || schema.Type != "object" || !schema.AdditionalProperties {
		t.Errorf("schema root does not define a forward-compatible hello payload")
	}
	if !reflect.DeepEqual(schema.Required, []string{"protocol", "server", "capabilities"}) {
		t.Errorf("required = %v", schema.Required)
	}
	if protocol := schema.Properties["protocol"]; protocol.Type != "string" || protocol.Const != Version {
		t.Errorf("protocol schema = %#v", protocol)
	}
	if server := schema.Properties["server"]; server.Type != "string" || server.MinLength != 1 {
		t.Errorf("server schema = %#v", server)
	}
	capabilities := schema.Properties["capabilities"]
	if capabilities.Type != "array" || !capabilities.UniqueItems || capabilities.Items == nil ||
		capabilities.Items.Type != "string" || capabilities.Items.MinLength != 1 {
		t.Errorf("capabilities schema = %#v", capabilities)
	}
}
