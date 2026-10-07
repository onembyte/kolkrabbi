package local

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCloudModelAliasUsesOllamaSourceSuffixRules(t *testing.T) {
	tests := map[string]string{
		"gpt-oss:120b":        "gpt-oss:120b-cloud",
		"glm-5.1":             "glm-5.1:cloud",
		"registry:5000/model": "registry:5000/model:cloud",
		"owner/model:latest":  "owner/model:latest-cloud",
		"glm-5.1:cloud":       "glm-5.1:cloud",
		"gpt-oss:120b-cloud":  "gpt-oss:120b-cloud",
	}
	for input, want := range tests {
		if got := cloudModelAlias(input); got != want {
			t.Errorf("cloudModelAlias(%q) = %q, want %q", input, got, want)
		}
	}
}

// Only an answer without a remote host says "not Cloud". A signed-out server,
// a failing or unreachable one, and a cancelled check each say something else,
// and none of them may be reported as the model's nature.
func TestCloudModelRemoteHostNeedsTheServersProof(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch {
		case strings.Contains(string(body), `"glm-5.1:cloud"`):
			_, _ = w.Write([]byte(`{"remote_host":"https://ollama.com:443"}`))
		case strings.Contains(string(body), `"missing:cloud"`):
			http.Error(w, `{"error":"model not found"}`, http.StatusNotFound)
		case strings.Contains(string(body), `"locked:cloud"`):
			http.Error(w, `{"error":"unauthorized","signin_url":"https://ollama.com/connect"}`, http.StatusUnauthorized)
		case strings.Contains(string(body), `"broken:cloud"`):
			http.Error(w, `{"error":"boom"}`, http.StatusInternalServerError)
		case strings.Contains(string(body), `"garbled:cloud"`):
			_, _ = w.Write([]byte(`not json`))
		case strings.Contains(string(body), `"slow:cloud"`):
			<-r.Context().Done()
		default:
			_, _ = w.Write([]byte(`{"capabilities":["completion"]}`))
		}
	}))
	defer server.Close()
	addr := strings.TrimPrefix(server.URL, "http://")
	ctx := context.Background()

	if got, err := CloudModelRemoteHost(ctx, addr, "glm-5.1:cloud"); err != nil || got != "https://ollama.com:443" {
		t.Errorf("proven Cloud model = %q, %v; want its remote host", got, err)
	}
	for _, name := range []string{"big:70b-cloud", "missing:cloud"} {
		if got, err := CloudModelRemoteHost(ctx, addr, name); got != "" || !errors.Is(err, ErrNotCloudModel) {
			t.Errorf("%s = %q, %v; want ErrNotCloudModel", name, got, err)
		}
	}
	if _, err := CloudModelRemoteHost(ctx, addr, "locked:cloud"); !errors.Is(err, ErrCloudSignedOut) {
		t.Errorf("a 401 = %v; want ErrCloudSignedOut", err)
	}
	for _, target := range []struct{ addr, name string }{{addr, "broken:cloud"}, {addr, "garbled:cloud"}, {"127.0.0.1:1", "glm-5.1:cloud"}} {
		if got, err := CloudModelRemoteHost(ctx, target.addr, target.name); got != "" || err == nil || errors.Is(err, ErrNotCloudModel) {
			t.Errorf("%s at %s = %q, %v; want a failure to ask, not a verdict", target.name, target.addr, got, err)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	time.AfterFunc(50*time.Millisecond, cancel)
	// Exactly the caller's own error, not a wrapped "asking ollama" failure.
	if _, err := CloudModelRemoteHost(cancelled, addr, "slow:cloud"); err == nil || err.Error() != context.Canceled.Error() {
		t.Errorf("a cancelled check = %v; want the caller's context.Canceled itself", err)
	}
}

func TestIsCloudModelNameAcceptsOnlyCloudSelectors(t *testing.T) {
	for name, want := range map[string]bool{
		"gpt-oss:120b-cloud": true, "glm-5.1:cloud": true, "owner/model:latest-cloud": true,
		"qwen2.5-coder:7b": false, "glm-5.1": false, "registry:5000/model": false,
		"cloud": false, "-cloud": false, "": false, "  ": false,
	} {
		if got := IsCloudModelName(name); got != want {
			t.Errorf("IsCloudModelName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestListCloudModelsRequiresRemoteProofAndUsesShowMetadata(t *testing.T) {
	server, shows := hostWithModels(t, "0.33.1", `{"models":[]}`, map[string]string{
		"gpt-oss:120b-cloud": `{"remote_model":"gpt-oss:120b","remote_host":"https://ollama.com:443",
      "capabilities":["completion","tools","thinking"],"details":{"context_length":131072}}`,
		"glm-5.1:cloud": `{"capabilities":["completion","tools"],"details":{"context_length":65536}}`,
		"missing:cloud": `{"error":"not found"}`,
	})
	catalog := []CloudCatalogModel{
		{Name: "gpt-oss:120b", Digest: "cloud-one", Size: 123, Parameters: "120B"},
		{Name: "glm-5.1", Digest: "cloud-two", Parameters: ""},
		{Name: "missing", Digest: "cloud-three"},
	}

	models, err := ListCloudModels(context.Background(), addrOf(server), "0.33.1", filepath.Join(t.TempDir(), "host.json"), catalog)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 {
		t.Fatalf("got %d cloud models, want only the remotely proven row: %+v", len(models), models)
	}
	model := models[0]
	if model.Name != "gpt-oss:120b-cloud" || !model.Cloud || !model.NotPulled || model.RemoteHost == "" {
		t.Errorf("cloud model = %+v, want normalized, remote, and not-pulled", model)
	}
	if model.ContextLength != 131072 || !model.Tools || !model.Thinking || !model.CapabilitiesKnown {
		t.Errorf("cloud model = %+v, want capabilities and context from /api/show", model)
	}
	if model.Parameters != "120B" || model.Size != 123 {
		t.Errorf("cloud model = %+v, want public catalogue metadata retained", model)
	}
	if shows.Load() != 3 {
		t.Fatalf("made %d /api/show calls, want one bounded probe per candidate", shows.Load())
	}
}

func TestListCloudModelsRejectsAnUnboundedCandidateList(t *testing.T) {
	server, shows := hostWithModels(t, "0.33.1", `{"models":[]}`, nil)
	catalog := make([]CloudCatalogModel, cloudCatalogMaxRows+1)
	for index := range catalog {
		catalog[index].Name = "model-" + strings.Repeat("x", 1)
	}

	if _, err := ListCloudModels(context.Background(), addrOf(server), "0.33.1", "", catalog); err == nil {
		t.Fatal("accepted a candidate list above the enrichment bound")
	}
	if shows.Load() != 0 {
		t.Fatalf("started %d show probes after rejecting the candidate bound", shows.Load())
	}
}

func TestListCloudModelsHonorsCancellation(t *testing.T) {
	server, _ := hostWithModels(t, "0.33.1", `{"models":[]}`, map[string]string{
		"model:cloud": `{"remote_host":"https://ollama.com:443"}`,
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := ListCloudModels(ctx, addrOf(server), "0.33.1", "", []CloudCatalogModel{{Name: "model"}}); err == nil {
		t.Fatal("a cancelled cloud enrichment returned nil error")
	}
}

func TestListCloudModelsCachesRemoteShowByDigest(t *testing.T) {
	server, shows := hostWithModels(t, "0.33.1", `{"models":[]}`, map[string]string{
		"gpt-oss:120b-cloud": `{"remote_host":"https://ollama.com:443","capabilities":["completion","tools"]}`,
	})
	cache := filepath.Join(t.TempDir(), "host.json")
	catalog := []CloudCatalogModel{{Name: "gpt-oss:120b", Digest: "cloud-one"}}

	if _, err := ListCloudModels(context.Background(), addrOf(server), "0.33.1", cache, catalog); err != nil {
		t.Fatal(err)
	}
	if _, err := ListCloudModels(context.Background(), addrOf(server), "0.33.1", cache, catalog); err != nil {
		t.Fatal(err)
	}
	if shows.Load() != 1 {
		t.Fatalf("made %d /api/show calls across cold and warm runs, want 1", shows.Load())
	}
}

func TestListCloudModelsProbesDuplicateAliasesOnce(t *testing.T) {
	server, shows := hostWithModels(t, "0.33.1", `{"models":[]}`, map[string]string{
		"gpt-oss:120b-cloud": `{"remote_host":"https://ollama.com:443","capabilities":["tools"]}`,
	})
	catalog := []CloudCatalogModel{
		{Name: "gpt-oss:120b", Digest: "cloud-one"},
		{Name: "gpt-oss:120b", Digest: "cloud-two"},
	}

	models, err := ListCloudModels(context.Background(), addrOf(server), "0.33.1", "", catalog)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || shows.Load() != 1 {
		t.Fatalf("duplicate alias was not collapsed before probing: shows=%d models=%+v", shows.Load(), models)
	}
}

func TestListCloudModelsRejectsAnOversizedShowResponse(t *testing.T) {
	server, _ := hostWithModels(t, "0.33.1", `{"models":[]}`, map[string]string{
		"gpt-oss:120b-cloud": `{"remote_host":"https://ollama.com:443"}` + strings.Repeat(" ", hostShowMaxBodyBytes),
	})

	models, err := ListCloudModels(context.Background(), addrOf(server), "0.33.1", "", []CloudCatalogModel{{Name: "gpt-oss:120b"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 0 {
		t.Fatalf("accepted an oversized /api/show response: %+v", models)
	}
}

func TestListCloudModelsDoesNotTrustANonRemoteCacheEntry(t *testing.T) {
	server, shows := hostWithModels(t, "0.33.1", `{"models":[]}`, map[string]string{
		"gpt-oss:120b-cloud": `{"remote_host":"https://ollama.com:443","capabilities":["tools"]}`,
	})
	cache := filepath.Join(t.TempDir(), "host.json")
	saveHostCache(cache, hostCatalogCache{
		Version: "0.33.1",
		Models: map[string]HostModel{
			"cloud-one": {Name: "gpt-oss:120b-cloud", Cloud: false, RemoteHost: "https://ollama.com:443"},
		},
	})

	models, err := ListCloudModels(context.Background(), addrOf(server), "0.33.1", cache, []CloudCatalogModel{{Name: "gpt-oss:120b", Digest: "cloud-one"}})
	if err != nil {
		t.Fatal(err)
	}
	if shows.Load() != 1 || len(models) != 1 || !models[0].Cloud {
		t.Fatalf("cache without cloud proof was trusted: shows=%d models=%+v", shows.Load(), models)
	}
}
