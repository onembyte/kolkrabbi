package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/secret"
)

func TestProviderClientConstructionFollowsEndpointPrecedence(t *testing.T) {
	tests := []struct {
		name    string
		flag    string
		env     string
		saved   string
		want    string
		wantKey bool
	}{
		{
			name:  "flag beats environment and saved config",
			flag:  "http://flag.invalid/v1",
			env:   "http://env.invalid/v1",
			saved: "http://saved.invalid/v1",
			want:  "http://flag.invalid/v1",
		},
		{
			name:  "environment beats saved config",
			env:   "http://env.invalid/v1",
			saved: "http://saved.invalid/v1",
			want:  "http://env.invalid/v1",
		},
		{
			name:  "saved config is used",
			saved: "http://saved.invalid/v1",
			want:  "http://saved.invalid/v1",
		},
		{
			name:    "default is authenticated OpenRouter",
			want:    provider.DefaultBaseURL,
			wantKey: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := storeFirstRunKey(t)
			t.Setenv("OPENROUTER_BASE_URL", tt.env)
			cfg := &config.Config{BaseURL: tt.saved}
			endpoint := config.ResolveBaseURL(tt.flag, cfg)

			client, err := providerClientForEndpoint(context.Background(), endpoint, d.CredentialsFile())
			if err != nil {
				t.Fatal(err)
			}
			if client.BaseURL != tt.want {
				t.Fatalf("client BaseURL = %q, want %q", client.BaseURL, tt.want)
			}
			if client.HasKey() != tt.wantKey {
				t.Fatalf("client HasKey = %v, want %v", client.HasKey(), tt.wantKey)
			}
			if tt.wantKey {
				if got := client.Key().Reveal(); got != firstRunStoredKey {
					t.Fatalf("OpenRouter key = %q, want stored credential", got)
				}
				if client.Origin != "" {
					t.Fatalf("OpenRouter client origin = %q", client.Origin)
				}
			} else if client.Origin != provider.CompatibleOrigin {
				t.Fatalf("compatible client origin = %q", client.Origin)
			}
		})
	}
}

// TestProviderClientEndpointMatrixDecidesKeyedOrKeyless is the startup half
// of the V34.1a matrix: for every endpoint shape a flag, environment variable,
// or saved config can supply, the client is either bound to canonical
// OpenRouter with the stored key, or credentialless — and a credentialless
// decision never opens the credential manifest at all. The keyless rows use a
// corrupt manifest so that any read would fail the construction.
func TestProviderClientEndpointMatrixDecidesKeyedOrKeyless(t *testing.T) {
	keyed := []string{
		provider.DefaultBaseURL,
		provider.DefaultBaseURL + "/",
		"https://OPENROUTER.AI/api/v1",
		"HTTPS://openrouter.ai/api/v1",
		"https://openrouter.ai:443/api/v1",
		"https://openrouter.ai/api/v1?trace=1",
		"https://openrouter.ai/api/v1#ignored",
	}
	keyless := []string{
		"https://openrouter.ai.evil/api/v1",
		"https://evil-openrouter.ai/api/v1",
		"https://evil.invalid/openrouter.ai/api/v1",
		"https://evil.invalid/api/v1?next=https://openrouter.ai",
		"https://openrouter.ai./api/v1",
		"https://openrouter.aİ/api/v1", // U+0130 folds to ASCII i under ToLower
		"https://OPENROUTER.Aİ/api/v1",
		"http://openrouter.ai/api/v1",
		"http://openrouter.ai:443/api/v1",
		"https://openrouter.ai:8443/api/v1",
		"https://openrouter.ai@evil.invalid/api/v1",
		"https://user:pass@openrouter.ai/api/v1",
		"https://sk-or-v1-userinfo-canary@openrouter.ai/api/v1",
		"http://127.0.0.1:11434/v1",
		"http://localhost:4000/v1",
	}

	for _, endpoint := range keyed {
		t.Run("keyed "+endpoint, func(t *testing.T) {
			d := storeFirstRunKey(t)
			client, err := providerClientForEndpoint(context.Background(), endpoint, d.CredentialsFile())
			if err != nil {
				t.Fatal(err)
			}
			if !client.HasKey() || client.Key().Reveal() != firstRunStoredKey {
				t.Fatalf("canonical endpoint %q did not receive the stored credential", endpoint)
			}
			if client.Origin != "" {
				t.Fatalf("canonical endpoint %q origin = %q, want OpenRouter", endpoint, client.Origin)
			}
		})
	}

	for _, endpoint := range keyless {
		t.Run("keyless "+endpoint, func(t *testing.T) {
			d := isolateHome(t)
			writeCorruptCredentialManifest(t, d.CredentialsFile())
			client, err := providerClientForEndpoint(context.Background(), endpoint, d.CredentialsFile())
			if err != nil {
				t.Fatalf("keyless endpoint %q read the credential manifest: %v", endpoint, err)
			}
			if client.HasKey() {
				t.Fatalf("replacement endpoint %q received a credential", endpoint)
			}
			if client.Origin != provider.CompatibleOrigin {
				t.Fatalf("replacement endpoint %q origin = %q, want compatible", endpoint, client.Origin)
			}
			if err := client.SetKey(secret.New("sk-or-v1-late-key")); !errors.Is(err, provider.ErrCredentialBinding) {
				t.Fatalf("late SetKey on %q = %v, want ErrCredentialBinding", endpoint, err)
			}
		})
	}
}

func TestProviderClientCanonicalEndpointWithoutAKeyIsGuidedNotKeyless(t *testing.T) {
	d := isolateHome(t)
	client, err := providerClientForEndpoint(context.Background(), provider.DefaultBaseURL, d.CredentialsFile())
	if client != nil {
		t.Fatal("canonical OpenRouter without a credential produced a client")
	}
	if err == nil || !strings.Contains(err.Error(), "/key") {
		t.Fatalf("error = %v, want the guided `/key` action", err)
	}
}

func TestModelsUsesCustomEndpointWithoutReadingOpenRouterCredential(t *testing.T) {
	d := isolateHome(t)
	writeCorruptCredentialManifest(t, d.CredentialsFile())

	var calls int
	var authorization string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		authorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"id":"custom/catalog-model","context_length":64000}]}`)
	}))
	defer srv.Close()

	if err := config.Save(d.ConfigFile(), &config.Config{BaseURL: srv.URL}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENROUTER_BASE_URL", srv.URL)
	a, out, _ := newTestApp(t, "")
	if err := a.runModels(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("custom catalog calls = %d, want 1", calls)
	}
	if authorization != "" {
		t.Fatalf("custom catalog received Authorization %q", authorization)
	}
	if !strings.Contains(out.String(), "custom/catalog-model") {
		t.Fatalf("custom model missing from output:\n%s", out)
	}
}
