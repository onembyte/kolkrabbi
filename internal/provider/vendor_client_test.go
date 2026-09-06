package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/secret"
)

// V34.4c.1: a key for an owner-chosen vendor origin. The key is bound to that
// vendor's documented API base and nowhere else, the client says which vendor
// it is, and a provider whose disposition is not a keyed origin gets no client.
func TestAVendorClientBindsItsKeyToTheVendorsOrigin(t *testing.T) {
	const key = "xai-vendor-origin-canary-0123456789"
	client, err := NewVendorClient("xai", key)
	if err != nil {
		t.Fatal(err)
	}
	if client.BaseURL != "https://api.x.ai/v1" || client.Origin != "xai" || !client.HasKey() {
		t.Fatalf("xai client = base %q origin %q key %v", client.BaseURL, client.Origin, client.HasKey())
	}
	recorder := &recordingTransport{}
	client.auth.Base = recorder
	if _, err := client.ListModels(context.Background()); err != nil {
		t.Fatal(err)
	}
	if recorder.last == nil || recorder.last.URL.String() != "https://api.x.ai/v1/models" {
		t.Fatalf("request = %v, want the vendor's models endpoint", recorder.last)
	}
	if got := recorder.last.Header.Get("Authorization"); got != "Bearer "+key {
		t.Fatalf("Authorization = %q", got)
	}

	// The same key never leaves for another host, even through the same client.
	client.BaseURL = "https://untrusted.invalid/v1"
	_, err = client.ListModels(context.Background())
	if !errors.Is(err, secret.ErrCredentialOrigin) || strings.Contains(err.Error(), key) {
		t.Fatalf("foreign host: err = %v, want the origin refusal without the key", err)
	}

	google, err := NewVendorClient("google", "AIza"+strings.Repeat("a", 35))
	if err != nil || google.BaseURL != "https://generativelanguage.googleapis.com/v1beta/openai" || google.Origin != "google" {
		t.Fatalf("google client = %+v, %v", google, err)
	}

	for _, provider := range []string{"perplexity", "mistral", "anthropic", "nobody"} {
		if c, err := NewVendorClient(provider, "k"); err == nil || c != nil {
			t.Fatalf("%s: got a keyed vendor client; its disposition is not a keyed origin", provider)
		}
	}
}

// Without a key a vendor client says which key, in the vendor's own words,
// not OpenRouter's.
func TestAVendorClientWithoutAKeyNamesTheVendorsKey(t *testing.T) {
	client, err := NewVendorClient("xai", "")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = client.StreamChat(context.Background(), "grok-4.6", []Message{{Role: "user", Content: "hi"}}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "XAI_API_KEY") || strings.Contains(err.Error(), "OPENROUTER") {
		t.Fatalf("no-key error = %v, want the vendor's env named", err)
	}
}
