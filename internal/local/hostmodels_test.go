package local

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// hostWithModels is a fake server with a tags list and a show answer per
// model, shaped the way Ollama 0.33 answers. shows counts /api/show calls so a
// test can prove the cache did its job.
func hostWithModels(t *testing.T, version string, tags string, shows map[string]string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var showCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			_, _ = w.Write([]byte("Ollama is running"))
		case "/api/version":
			_, _ = w.Write([]byte(`{"version":"` + version + `"}`))
		case "/api/tags":
			_, _ = w.Write([]byte(tags))
		case "/api/show":
			showCalls.Add(1)
			var req struct {
				Model string `json:"model"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if body, ok := shows[req.Model]; ok {
				_, _ = w.Write([]byte(body))
				return
			}
			http.Error(w, `{"error":"model not found"}`, http.StatusNotFound)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, &showCalls
}

const twoModelTags = `{"models":[
 {"name":"qwen2.5-coder:7b","model":"qwen2.5-coder:7b","size":4683087332,"digest":"aaa111",
  "details":{"family":"qwen2","parameter_size":"7.6B","quantization_level":"Q4_K_M"}},
 {"name":"gpt-oss:120b-cloud","model":"gpt-oss:120b-cloud","size":0,"digest":"bbb222",
  "remote_model":"gpt-oss:120b","remote_host":"https://ollama.com",
  "details":{"family":"gptoss","parameter_size":"120B"}}
]}`

var twoModelShows = map[string]string{
	"qwen2.5-coder:7b": `{"capabilities":["completion","tools"],
	  "details":{"family":"qwen2","parameter_size":"7.6B","quantization_level":"Q4_K_M","context_length":32768},
	  "model_info":{"general.architecture":"qwen2","qwen2.context_length":32768}}`,
	"gpt-oss:120b-cloud": `{"capabilities":["completion","tools","thinking"],
	  "details":{"family":"gptoss","parameter_size":"120B"},
	  "model_info":{"general.architecture":"gptoss","gptoss.context_length":131072}}`,
}

func TestListHostModelsDecodesTagsAndShow(t *testing.T) {
	server, _ := hostWithModels(t, "0.33.1", twoModelTags, twoModelShows)
	models, err := ListHostModels(context.Background(), addrOf(server), filepath.Join(t.TempDir(), "host.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("decoded %d models, want 2: %+v", len(models), models)
	}
	local, cloud := models[0], models[1]
	if local.Name != "qwen2.5-coder:7b" || local.Parameters != "7.6B" || local.Quantization != "Q4_K_M" {
		t.Errorf("local = %+v, want name, parameters and quantisation from the tags", local)
	}
	if local.ContextLength != 32768 || !local.Tools || local.Cloud {
		t.Errorf("local = %+v, want context from details, tools from capabilities, not cloud", local)
	}
	if !cloud.Cloud || cloud.ContextLength != 131072 || !cloud.Thinking {
		t.Errorf("cloud = %+v, want cloud from remote_host, context from model_info, thinking from capabilities", cloud)
	}
}

// An empty server says "models": null on /v1/models and may on /api/tags; that
// is a user with nothing pulled, not a broken server.
func TestListHostModelsTreatsNullAsEmpty(t *testing.T) {
	server, _ := hostWithModels(t, "0.33.1", `{"models":null}`, nil)
	models, err := ListHostModels(context.Background(), addrOf(server), filepath.Join(t.TempDir(), "host.json"))
	if err != nil {
		t.Fatalf("an empty server was reported as an error: %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("decoded %d models from nothing", len(models))
	}
}

// The guard that matters for routing: a model whose capabilities are unknown
// must not claim tools. Before 0.6.4 /api/show has no capabilities array, so
// nothing can be claimed — and a slot ranker that trusted a claim here would
// send tool schemas to a model that will 400 on them.
func TestListHostModelsClaimsNoToolsWhenCapabilitiesAreUnknown(t *testing.T) {
	oldShows := map[string]string{
		"qwen2.5-coder:7b": `{"details":{"context_length":32768},"model_info":{}}`,
	}
	server, _ := hostWithModels(t, "0.5.0", twoModelTags, oldShows)
	models, err := ListHostModels(context.Background(), addrOf(server), filepath.Join(t.TempDir(), "host.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range models {
		if m.Tools || m.CapabilitiesKnown {
			t.Errorf("%s claims tools=%v known=%v on a server that cannot say", m.Name, m.Tools, m.CapabilitiesKnown)
		}
		if !strings.Contains(m.ModelInfo().Description, "unknown") {
			t.Errorf("%s does not say its capabilities are unknown: %q", m.Name, m.ModelInfo().Description)
		}
	}
}

// One model that /api/show cannot describe must not take the list with it.
func TestListHostModelsKeepsAModelWhoseShowFailed(t *testing.T) {
	shows := map[string]string{"qwen2.5-coder:7b": twoModelShows["qwen2.5-coder:7b"]}
	server, _ := hostWithModels(t, "0.33.1", twoModelTags, shows)
	models, err := ListHostModels(context.Background(), addrOf(server), filepath.Join(t.TempDir(), "host.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("a failed show dropped a model: %d listed", len(models))
	}
	if models[1].Tools || models[1].CapabilitiesKnown {
		t.Errorf("the model whose show failed claims tools: %+v", models[1])
	}
}

func TestHostModelProjectsToModelInfoWithTheRoutePrefix(t *testing.T) {
	local := HostModel{Name: "qwen2.5-coder:7b", Parameters: "7.6B", Quantization: "Q4_K_M", ContextLength: 32768, Tools: true, CapabilitiesKnown: true}
	info := local.ModelInfo()
	if info.ID != "ollama/qwen2.5-coder:7b" {
		t.Errorf("id = %q, want the route prefix the engine strips", info.ID)
	}
	if !provider.SupportsTools(info) || !provider.ModelIsFree(info) || info.ContextLength != 32768 {
		t.Errorf("projection lost tools, freeness or context: %+v", info)
	}
	cloud := HostModel{Name: "gpt-oss:120b-cloud", Cloud: true, Tools: true, CapabilitiesKnown: true}
	if provider.ModelIsFree(cloud.ModelInfo()) {
		t.Error("a cloud model projected as free; it bills against the Ollama plan")
	}
	noTools := HostModel{Name: "gemma2:9b", CapabilitiesKnown: true}
	if provider.SupportsTools(noTools.ModelInfo()) {
		t.Error("a model without the tools capability projected as tool-capable")
	}
}

// /api/tags is one request; /api/show is one per model. The cache keeps the
// per-model answers by digest, so a second startup pays for the list alone.
func TestListHostModelsCachesShowByDigest(t *testing.T) {
	server, shows := hostWithModels(t, "0.33.1", twoModelTags, twoModelShows)
	cache := filepath.Join(t.TempDir(), "host.json")
	if _, err := ListHostModels(context.Background(), addrOf(server), cache); err != nil {
		t.Fatal(err)
	}
	if shows.Load() != 2 {
		t.Fatalf("cold run made %d show calls, want 2", shows.Load())
	}
	models, err := ListHostModels(context.Background(), addrOf(server), cache)
	if err != nil {
		t.Fatal(err)
	}
	if shows.Load() != 2 {
		t.Fatalf("warm run made %d more show calls, want 0: nothing changed", shows.Load()-2)
	}
	if len(models) != 2 || !models[0].Tools {
		t.Fatalf("the cached answer lost detail: %+v", models)
	}
}
