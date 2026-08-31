package local

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestFetchCloudCatalogDecodesTags(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/tags" {
			t.Errorf("request = %s %s, want GET /api/tags", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("catalog request carried an authorization header %q", got)
		}
		_, _ = fmt.Fprint(w, `{"models":[
      {"name":"gpt-oss:120b","model":"gpt-oss:120b","size":1234,"digest":"sha256:one",
       "details":{"family":"gptoss","parameter_size":"120B","quantization_level":"Q4_K_M"}},
      {"name":"glm-5.1","model":"glm-5.1","size":0,"digest":"sha256:two","details":{}}
    ]}`)
	}))
	t.Cleanup(server.Close)

	models, err := fetchCloudCatalog(context.Background(), server.Client(), server.URL+"/api/tags")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("decoded %d models, want 2: %+v", len(models), models)
	}
	if models[0].Name != "gpt-oss:120b" || models[0].Size != 1234 || models[0].Digest != "sha256:one" {
		t.Errorf("first model = %+v, want name, size and digest", models[0])
	}
	if models[0].Parameters != "120B" || models[0].Quantization != "Q4_K_M" {
		t.Errorf("first model = %+v, want public details", models[0])
	}
}

func TestFetchCloudCatalogAcceptsNullModelsAsEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"models":null}`)
	}))
	t.Cleanup(server.Close)

	models, err := fetchCloudCatalog(context.Background(), server.Client(), server.URL+"/api/tags")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 0 {
		t.Fatalf("decoded %d models from null, want empty", len(models))
	}
}

func TestFetchCloudCatalogRejectsNullDocument(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `null`)
	}))
	t.Cleanup(server.Close)

	if _, err := fetchCloudCatalog(context.Background(), server.Client(), server.URL+"/api/tags"); err == nil {
		t.Fatal("accepted a null catalog document")
	}
}

func TestFetchCloudCatalogRejectsMalformedOversizedAndNonOKResponses(t *testing.T) {
	tests := []struct {
		name string
		body string
		code int
	}{
		{name: "malformed", body: `{"models":[`, code: http.StatusOK},
		{name: "oversized", body: `{"models":[]}` + strings.Repeat(" ", cloudCatalogMaxBodyBytes), code: http.StatusOK},
		{name: "unauthorized", body: `{"error":"no"}`, code: http.StatusUnauthorized},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.code)
				_, _ = fmt.Fprint(w, test.body)
			}))
			t.Cleanup(server.Close)

			if _, err := fetchCloudCatalog(context.Background(), server.Client(), server.URL+"/api/tags"); err == nil {
				t.Fatalf("accepted %s response", test.name)
			}
		})
	}
}

func TestFetchCloudCatalogRejectsTooManyRowsAndUnsafeNames(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "too many rows", body: `{"models":[` + strings.TrimSuffix(strings.Repeat(`{"name":"model"},`, cloudCatalogMaxRows+1), ",") + `]}`},
		{name: "empty name", body: `{"models":[{"name":" "}]}`},
		{name: "control character", body: `{"models":[{"name":"model\nnext"}]}`},
		{name: "long name", body: `{"models":[{"name":"` + strings.Repeat("m", cloudCatalogMaxNameBytes+1) + `"}]}`},
		{name: "long metadata", body: `{"models":[{"name":"model","digest":"` + strings.Repeat("d", cloudCatalogMaxFieldBytes+1) + `"}]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprint(w, test.body)
			}))
			t.Cleanup(server.Close)

			if _, err := fetchCloudCatalog(context.Background(), server.Client(), server.URL+"/api/tags"); err == nil {
				t.Fatalf("accepted %s response", test.name)
			}
		})
	}
}

func TestFetchCloudCatalogHonorsCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := fetchCloudCatalog(ctx, server.Client(), server.URL+"/api/tags"); err == nil {
		t.Fatal("a cancelled catalog request returned nil error")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("catalog cancellation took %v", elapsed)
	}
}

func TestFetchCloudCatalogDoesNotFollowRedirects(t *testing.T) {
	var followed bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		followed = true
	}))
	t.Cleanup(target.Close)
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL+"/api/tags")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(redirect.Close)

	if _, err := fetchCloudCatalog(context.Background(), redirect.Client(), redirect.URL+"/api/tags"); err == nil {
		t.Fatal("accepted a redirect as a catalog response")
	}
	if followed {
		t.Fatal("catalog client followed a redirect")
	}
}

func TestFetchCloudCatalogDoesNotSendCallerCookies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := r.Cookie("session"); err == nil {
			t.Error("catalog request carried a caller cookie")
		}
		_, _ = fmt.Fprint(w, `{"models":[]}`)
	}))
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	origin, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	jar.SetCookies(origin, []*http.Cookie{{Name: "session", Value: "secret"}})
	client := server.Client()
	client.Jar = jar

	if _, err := fetchCloudCatalog(context.Background(), client, server.URL+"/api/tags"); err != nil {
		t.Fatal(err)
	}
}
