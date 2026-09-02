package cli

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/provider"
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
