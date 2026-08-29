package local

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// windowServer is a fake host that serves one turn, reports what is loaded on
// /api/ps, and describes models on /api/show — enough to see where the
// backend gets a window from.
func windowServer(t *testing.T, ps string, shows map[string]string) (*httptest.Server, *atomic.Int32, *[]string) {
	t.Helper()
	var psCalls atomic.Int32
	var generated []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			_, _ = w.Write([]byte("Ollama is running"))
		case "/api/version":
			_, _ = w.Write([]byte(`{"version":"0.33.1"}`))
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
		case "/api/ps":
			psCalls.Add(1)
			_, _ = w.Write([]byte(ps))
		case "/api/show":
			var req struct {
				Model string `json:"model"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if body, ok := shows[req.Model]; ok {
				_, _ = w.Write([]byte(body))
				return
			}
			http.NotFound(w, r)
		case "/api/generate":
			var req struct {
				Model     string `json:"model"`
				KeepAlive string `json:"keep_alive"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			generated = append(generated, req.Model+" keep_alive="+req.KeepAlive)
			_, _ = w.Write([]byte(`{"model":"` + req.Model + `","done":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, &psCalls, &generated
}

const loadedAt8k = `{"models":[{"name":"qwen2.5-coder:7b","model":"qwen2.5-coder:7b","size":5000000000,"context_length":8192}]}`

// E8. Ollama truncates from the front, which drops kolk's system prompt and
// tool schemas first — so a window that is unknown must be treated as small,
// and the real one read from the server as soon as it can be.
func TestHostBackendTreatsAnUnknownWindowAsSmall(t *testing.T) {
	server, _, _ := windowServer(t, `{"models":[]}`, nil)
	backend := NewHostBackend(addrOf(server))
	if got := backend.ContextWindow("qwen2.5-coder:7b"); got != hostFloorContext {
		t.Fatalf("window before any turn = %d, want the floor %d", got, hostFloorContext)
	}
}

func TestHostBackendReadsTheEffectiveWindowAfterTheFirstTurn(t *testing.T) {
	server, psCalls, _ := windowServer(t, loadedAt8k, nil)
	backend := NewHostBackend(addrOf(server))
	for range 3 {
		if _, _, err := backend.StreamChat(context.Background(), "qwen2.5-coder:7b", nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if got := backend.ContextWindow("qwen2.5-coder:7b"); got != 8192 {
		t.Fatalf("window after a turn = %d, want 8192 from /api/ps — the trained size is not what the server runs", got)
	}
	if psCalls.Load() != 1 {
		t.Fatalf("/api/ps asked %d times across three turns, want once per model", psCalls.Load())
	}
}

// A cloud model is never "loaded" here, so /api/ps has nothing to say; its
// window is the trained one the proxy reports through /api/show.
func TestHostBackendUsesTheTrainedWindowForACloudModel(t *testing.T) {
	shows := map[string]string{"gpt-oss:120b-cloud": `{"remote_host":"https://ollama.com","capabilities":["completion","tools"],"model_info":{"general.architecture":"gptoss","gptoss.context_length":131072}}`}
	server, _, _ := windowServer(t, `{"models":[]}`, shows)
	backend := NewHostBackend(addrOf(server))
	if _, _, err := backend.StreamChat(context.Background(), "gpt-oss:120b-cloud", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := backend.ContextWindow("gpt-oss:120b-cloud"); got != 131072 {
		t.Fatalf("cloud window = %d, want 131072", got)
	}
}

// Warming on selection loads the model so the first turn is not a cold load,
// and records the window the load produced.
func TestWarmLoadsTheModelAndRecordsItsWindow(t *testing.T) {
	server, _, generated := windowServer(t, loadedAt8k, nil)
	backend := NewHostBackend(addrOf(server))
	backend.Warm(context.Background(), "qwen2.5-coder:7b")
	if len(*generated) != 1 || !strings.HasPrefix((*generated)[0], "qwen2.5-coder:7b keep_alive=") || strings.HasSuffix((*generated)[0], "keep_alive=") {
		t.Fatalf("warm sent %v, want one generate for the model with a keep_alive", *generated)
	}
	if got := backend.ContextWindow("qwen2.5-coder:7b"); got != 8192 {
		t.Fatalf("window after warm = %d, want 8192", got)
	}
}
