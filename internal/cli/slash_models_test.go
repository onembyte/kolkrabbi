package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/local"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/session"
)

func TestSlashModelsRefreshDiscoversBeforeListingWithoutChangingTheSession(t *testing.T) {
	dirs := isolateConnectorState(t)
	signInAs(t, dirs, "openai", "ChatGPT Plus", "codex")
	a, out, errOut := newTestApp(t, "")
	a.dirs = dirs
	// Catalog metadata only: any inference request fails the rehearsal.
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/models" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "metadata only", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"id":"vendorless/model","context_length":1000}]}`)
	}))
	defer gateway.Close()
	a.sessionEndpoint = gateway.URL
	a.discoverHost = func(context.Context) local.Host { return local.Host{State: local.HostAbsent} }
	var store provider.VendorCatalogs
	store.Replace(provider.VendorCatalog{Vendor: "codex", FetchedAt: time.Now(),
		Models: []provider.DiscoveredModel{{ID: "old-entry", Status: provider.StatusListed}}})
	if err := provider.SaveVendorCatalogs(dirs.VendorCatalogFile(), store); err != nil {
		t.Fatal(err)
	}
	calls := 0
	a.modelLister = func(vendor string, _ []provider.ModelInfo) provider.ModelLister {
		if vendor != "codex" {
			t.Fatalf("unexpected vendor %s", vendor)
		}
		return fakeVendorLister{calls: &calls, catalog: provider.VendorCatalog{Vendor: "codex", Source: "test vendor catalog",
			Models: []provider.DiscoveredModel{{ID: "fresh-entry", Status: provider.StatusListed}}}}
	}
	ag := engine.New(engine.Options{Model: "mock/model", Mode: "code", Effort: "medium", Sess: session.New(t.TempDir(), "mock/model")})
	before := ag.Sess.GetMessages()
	for _, command := range []string{"/models", "/models --refresh codex", "/models codex"} {
		out.Reset()
		if a.slash(context.Background(), ag, command) {
			t.Fatalf("%s exited the session", command)
		}
		want := "old-entry"
		if command != "/models" {
			want = "fresh-entry"
		}
		if !strings.Contains(out.String(), want) || strings.Contains(out.String(), "unknown command") {
			t.Fatalf("%s did not reach model listing: %s", command, out.String())
		}
		if command != "/models" && strings.Contains(out.String(), "old-entry") {
			t.Fatalf("refresh printed the stale vendor row: %s", out.String())
		}
	}
	if !reflect.DeepEqual(ag.Sess.GetMessages(), before) {
		t.Fatal("model listing changed the conversation")
	}
	if calls != 1 || ag.SessionModel() != "mock/model" || ag.Effort != "medium" || errOut.Len() != 0 {
		t.Fatalf("refresh changed session or missed force: calls=%d model=%s effort=%s stderr=%s", calls, ag.SessionModel(), ag.Effort, errOut.String())
	}
}

func TestModelsRefreshAppearsInHelpAndCompletion(t *testing.T) {
	var help strings.Builder
	printSlashHelp(&help)
	if !strings.Contains(help.String(), "/models") || !strings.Contains(help.String(), "--refresh") {
		t.Fatalf("missing refresh help: %s", help.String())
	}
	for _, spec := range slashSuggestions() {
		if spec.Name == "models" && hasWord(spec, "--refresh") {
			return
		}
	}
	t.Fatal("/models --refresh is not discoverable through slash completion")
}

func TestModelsPublicHelpAndRetiredVerbPointToCatalogRefresh(t *testing.T) {
	for _, name := range []string{"models", "/models"} {
		t.Run(name, func(t *testing.T) {
			a, out, _ := newTestApp(t, "")
			if code := a.main(t.Context(), []string{"help", name}); code != ExitOK {
				t.Fatalf("help exit = %d", code)
			}
			if !strings.Contains(out.String(), "usage: /models [--refresh] [filter]") {
				t.Fatalf("catalog help was redirected to switching: %s", out.String())
			}
		})
	}
	a, out, errOut := newTestApp(t, "")
	if code := a.main(t.Context(), []string{"models", "--refresh"}); code == ExitOK {
		t.Fatal("a fifth outside-session verb was introduced")
	}
	if !strings.Contains(out.String()+errOut.String(), "is a session command now: /models\n") {
		t.Fatalf("retired verb gives the wrong command: %s%s", out.String(), errOut.String())
	}
}
