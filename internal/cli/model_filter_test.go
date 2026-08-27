package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/session"
)

func TestSlashModelTextFiltersCatalog(t *testing.T) {
	isolateHome(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[`+
			`{"id":"moonshotai/kimi-k2","name":"Kimi K2","context_length":128000,"pricing":{"prompt":"0","completion":"0"}},`+
			`{"id":"moonshotai/kimi-k1","name":"Kimi K1","context_length":128000,"pricing":{"prompt":"0","completion":"0"}},`+
			`{"id":"google/gemini-2.5-flash","name":"Gemini Flash","context_length":1000000,"pricing":{"prompt":"0","completion":"0"}}]}`)
	}))
	defer srv.Close()
	client := provider.NewClient("test-key")
	client.BaseURL = srv.URL
	a, out, errOut := newTestApp(t, "")
	ag := engine.New(engine.Options{Client: client, Model: "current/model", Sess: session.New(t.TempDir(), "current/model"), Out: io.Discard})

	if a.slash(context.Background(), ag, "/model kim") {
		t.Fatal("filtered /model must not exit the REPL")
	}
	if !strings.Contains(out.String(), "moonshotai/kimi-k2") ||
		!strings.Contains(out.String(), "moonshotai/kimi-k1") ||
		strings.Contains(out.String(), "google/gemini-2.5-flash") {
		t.Fatalf("filtered catalog output = %q", out.String())
	}
	if ag.Model != "current/model" || errOut.Len() != 0 {
		t.Fatalf("filter changed session or wrote stderr: model=%q stderr=%q", ag.Model, errOut.String())
	}
}
