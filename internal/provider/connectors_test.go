package provider

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveAndLoadConnectorIsCredentialFree(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "connectors.json")
	err := SaveConnector(context.Background(), path, Connector{
		Provider: "google", Plan: "Google AI Pro", Name: "gemini",
		Sandbox: true, LoginOwner: "provider-cli", Enabled: true,
		UpdatedAt: time.Unix(10, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := LoadConnectors(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Connectors) != 1 || manifest.Connectors[0].Plan != "Google AI Pro" {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "token") || strings.Contains(string(body), "credential") {
		t.Fatalf("connector manifest contains secret-shaped data: %s", body)
	}
}

func TestSaveConnectorUpsertsByProviderAndName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connectors.json")
	base := Connector{Provider: "google", Plan: "Gemini Free", Name: "gemini", LoginOwner: "provider-cli"}
	if err := SaveConnector(context.Background(), path, base); err != nil {
		t.Fatal(err)
	}
	base.Plan = "Google AI Pro"
	if err := SaveConnector(context.Background(), path, base); err != nil {
		t.Fatal(err)
	}
	got, err := LoadConnectors(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Connectors) != 1 || got.Connectors[0].Plan != "Google AI Pro" {
		t.Fatalf("upsert failed: %+v", got.Connectors)
	}
}

func TestLoadConnectorsRejectsCorruptAndWrongVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connectors.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConnectors(path); err == nil {
		t.Fatal("corrupt connector JSON should fail")
	}
	if err := os.WriteFile(path, []byte(`{"version":99}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConnectors(path); err == nil {
		t.Fatal("unsupported connector version should fail")
	}
}
