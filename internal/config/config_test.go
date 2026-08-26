package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestConfigSchemaAndWritesContainNoCredentialField(t *testing.T) {
	credentialName := regexp.MustCompile(`(?i)key|token|secret|credential`)
	typ := reflect.TypeOf(Config{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		jsonName, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if credentialName.MatchString(field.Name) || credentialName.MatchString(jsonName) {
			t.Errorf("Config exposes credential-shaped field %s (%q)", field.Name, jsonName)
		}
	}

	path := filepath.Join(t.TempDir(), "config", "config.json")
	if err := Save(path, &Config{Model: "openrouter/auto", BaseURL: "https://example.test"}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var written map[string]json.RawMessage
	if err := json.Unmarshal(body, &written); err != nil {
		t.Fatal(err)
	}
	for name := range written {
		if credentialName.MatchString(name) {
			t.Errorf("Save wrote forbidden setting %q: %s", name, body)
		}
	}
}

func TestSlotsRoundTrip(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.json")
	if err := Save(file, &Config{Slots: map[string]string{"explore": "cheap/reader"}}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(file)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Slots["explore"]; got != "cheap/reader" {
		t.Fatalf("slots = %v", reloaded.Slots)
	}
}
