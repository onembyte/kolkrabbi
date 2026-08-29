package local

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func pullServer(t *testing.T, lines []string, status int) (*httptest.Server, *[]byte) {
	t.Helper()
	var sent []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/pull" {
			http.NotFound(w, r)
			return
		}
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		sent = body[:n]
		w.WriteHeader(status)
		for _, line := range lines {
			_, _ = w.Write([]byte(line + "\n"))
		}
	}))
	t.Cleanup(server.Close)
	return server, &sent
}

// E10. A pull goes through the host's own API, streamed, so the person who
// approved it watches it happen rather than staring at a cursor for a 4 GB
// download.
func TestPullHostModelStreamsProgressAndEndsOnSuccess(t *testing.T) {
	server, sent := pullServer(t, []string{
		`{"status":"pulling manifest"}`,
		`{"status":"pulling sha256:abc","digest":"sha256:abc","total":1000,"completed":100}`,
		`{"status":"pulling sha256:abc","digest":"sha256:abc","total":1000,"completed":1000}`,
		`{"status":"verifying sha256 digest"}`,
		`{"status":"success"}`,
	}, http.StatusOK)
	var progress strings.Builder
	if err := PullHostModel(context.Background(), addrOf(server), "qwen2.5-coder:7b", &progress); err != nil {
		t.Fatal(err)
	}
	var req struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if json.Unmarshal(*sent, &req) != nil || req.Model != "qwen2.5-coder:7b" || !req.Stream {
		t.Fatalf("request = %s, want the model with streaming", *sent)
	}
	for _, want := range []string{"pulling manifest", "100%", "success"} {
		if !strings.Contains(progress.String(), want) {
			t.Errorf("progress lacks %q:\n%s", want, progress.String())
		}
	}
}

// The guard that matters: an error in the stream is a failed pull, not a
// finished one. Ollama reports failures as a line with "error", after 200 OK.
func TestPullHostModelFailsOnAnErrorLine(t *testing.T) {
	server, _ := pullServer(t, []string{
		`{"status":"pulling manifest"}`,
		`{"error":"pull model manifest: file does not exist"}`,
	}, http.StatusOK)
	err := PullHostModel(context.Background(), addrOf(server), "nope:latest", &strings.Builder{})
	if err == nil {
		t.Fatal("a pull whose stream reported an error was reported as done")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error %q lost the server's reason", err)
	}
}
