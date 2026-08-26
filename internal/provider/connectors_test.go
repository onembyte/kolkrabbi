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

func TestSaveConnectorStampsUpdatedAt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connectors.json")
	before := time.Now().UTC()
	if err := SaveConnector(context.Background(), path, Connector{
		Provider: "anthropic", Plan: "Claude Max", Name: "claude",
		LoginOwner: "provider-cli", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	manifest, err := LoadConnectors(path)
	if err != nil || len(manifest.Connectors) != 1 {
		t.Fatalf("manifest = %+v, err = %v", manifest, err)
	}
	stamped := manifest.Connectors[0].UpdatedAt
	if stamped.IsZero() {
		t.Fatal("a saved connector must record when it was written")
	}
	if stamped.Before(before.Add(-time.Second)) || stamped.After(time.Now().UTC().Add(time.Minute)) {
		t.Fatalf("updated_at = %s, want a current UTC instant", stamped)
	}
}

func TestSaveConnectorPreservesAnExplicitUpdatedAt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connectors.json")
	want := time.Date(2026, 8, 26, 5, 30, 0, 0, time.UTC)
	if err := SaveConnector(context.Background(), path, Connector{
		Provider: "anthropic", Plan: "Claude Max", Name: "claude",
		LoginOwner: "provider-cli", Enabled: true, UpdatedAt: want,
	}); err != nil {
		t.Fatal(err)
	}

	manifest, err := LoadConnectors(path)
	if err != nil || len(manifest.Connectors) != 1 {
		t.Fatalf("manifest = %+v, err = %v", manifest, err)
	}
	if got := manifest.Connectors[0].UpdatedAt; !got.Equal(want) {
		t.Fatalf("updated_at = %s, want %s", got, want)
	}
}
