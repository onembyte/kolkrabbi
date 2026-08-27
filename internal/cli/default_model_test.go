package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

func catalogModel(id, name, description, prompt, completion string, tools bool) provider.ModelInfo {
	model := provider.ModelInfo{ID: id, Name: name, Description: description, ContextLength: 128_000}
	model.Pricing.Prompt = prompt
	model.Pricing.Completion = completion
	if tools {
		model.SupportedParameters = []string{"tools"}
	}
	return model
}

func TestChooseBestDefaultModelPrefersFreeCodingAndToolCapability(t *testing.T) {
	models := []provider.ModelInfo{
		catalogModel(legacyFreePreset, "Former Free Alias", "best coding software engineering terminal coder", "0", "0", true),
		catalogModel("paid/frontier-code", "Frontier Code", "agentic software engineering", "0.000001", "0.000003", true),
		catalogModel("free/general:free", "General Free", "general reasoning assistant", "0", "0", true),
		catalogModel("free/no-tools-code:free", "Coder", "coding and terminal work", "0", "0", false),
		catalogModel("free/code:free", "North Code", "agentic coding and software engineering for terminal tasks", "0", "0", true),
	}

	choice, ok := chooseBestDefaultModel(models)
	if !ok {
		t.Fatal("catalog produced no default model")
	}
	if choice.Model != "free/code:free" || !choice.Free {
		t.Fatalf("default choice = %#v, want free/code:free", choice)
	}
}

func TestChooseBestDefaultModelFallsBackInSafeOrder(t *testing.T) {
	t.Run("unknown tool metadata on free model still beats verified paid model", func(t *testing.T) {
		models := []provider.ModelInfo{
			catalogModel("paid/code", "Paid Coder", "coding specialist", "0.000001", "0.000002", true),
			catalogModel("free/unknown-tools:free", "Free Coder", "coding specialist", "0", "0", false),
		}
		models[1].SupportedParameters = nil
		choice, ok := chooseBestDefaultModel(models)
		if !ok || choice.Model != "free/unknown-tools:free" || !choice.Free {
			t.Fatalf("default choice = %#v, %v", choice, ok)
		}
	})

	t.Run("free tool model before paid coding", func(t *testing.T) {
		models := []provider.ModelInfo{
			catalogModel("paid/code", "Paid Coder", "coding specialist", "0.000001", "0.000002", true),
			catalogModel("free/general:free", "Free General", "general assistant", "0", "0", true),
		}
		choice, ok := chooseBestDefaultModel(models)
		if !ok || choice.Model != "free/general:free" || !choice.Free {
			t.Fatalf("default choice = %#v, %v", choice, ok)
		}
	})

	t.Run("cheapest paid tool model only when no free model exists", func(t *testing.T) {
		models := []provider.ModelInfo{
			catalogModel("paid/expensive", "Expensive Coder", "coding specialist", "0.00001", "0.00003", true),
			catalogModel("paid/cheap", "Cheap Coder", "coding specialist", "0.0000001", "0.0000002", true),
		}
		choice, ok := chooseBestDefaultModel(models)
		if !ok || choice.Model != "paid/cheap" || choice.Free {
			t.Fatalf("default choice = %#v, %v", choice, ok)
		}
	})
}

func TestDiscoverDefaultModelUsesRankedCatalogAndSafeFreeRouterFallback(t *testing.T) {
	t.Run("ranked free coding model", func(t *testing.T) {
		var query string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			query = r.URL.RawQuery
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":[{"id":"cohere/north-code:free","name":"North Code","description":"agentic coding and terminal tasks","context_length":256000,"supported_parameters":["tools"],"pricing":{"prompt":"0","completion":"0","request":"0"}}]}`)
		}))
		defer srv.Close()

		client := provider.NewClient("test-key")
		client.BaseURL = srv.URL
		choice := discoverDefaultModel(context.Background(), client)
		if choice.Model != "cohere/north-code:free" || !choice.Free || choice.Warning != "" {
			t.Fatalf("discovered choice = %#v", choice)
		}
		for _, want := range []string{"sort=intelligence-high-to-low", "supported_parameters=tools"} {
			if !strings.Contains(query, want) {
				t.Fatalf("ranked catalog query %q omitted %q", query, want)
			}
		}
	})

	t.Run("catalog outage stays free", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}))
		defer srv.Close()

		client := provider.NewClient("test-key")
		client.BaseURL = srv.URL
		choice := discoverDefaultModel(context.Background(), client)
		if choice.Model != defaultModel || !choice.Free || !strings.Contains(choice.Warning, "free router") {
			t.Fatalf("outage fallback = %#v", choice)
		}
	})
}

func TestNewAgentDiscoversOnlyWhenNoUserOrSessionModelExists(t *testing.T) {
	t.Run("new session uses discovered free coding model", func(t *testing.T) {
		storeFirstRunKey(t)
		a, _, errOut := newTestApp(t, "")
		calls := 0
		a.chooseDefault = func(context.Context, *provider.Client) defaultModelChoice {
			calls++
			return defaultModelChoice{Model: "free/best-code", Free: true}
		}
		agent, err := a.newAgent(context.Background(), &options{})
		if err != nil {
			t.Fatal(err)
		}
		if calls != 1 || agent.Model != "free/best-code" || agent.Sess.ModelName() != "free/best-code" || errOut.Len() != 0 {
			t.Fatalf("calls/model/session/stderr = %d/%q/%q/%q", calls, agent.Model, agent.Sess.ModelName(), errOut.String())
		}
	})

	t.Run("configured model bypasses discovery", func(t *testing.T) {
		dirs := storeFirstRunKey(t)
		if err := config.Save(dirs.ConfigFile(), &config.Config{Model: "user/chosen"}); err != nil {
			t.Fatal(err)
		}
		a, _, _ := newTestApp(t, "")
		calls := 0
		a.chooseDefault = func(context.Context, *provider.Client) defaultModelChoice {
			calls++
			return defaultModelChoice{Model: "free/ignored", Free: true}
		}
		agent, err := a.newAgent(context.Background(), &options{})
		if err != nil {
			t.Fatal(err)
		}
		if calls != 0 || agent.Model != "user/chosen" {
			t.Fatalf("configured selection calls/model = %d/%q", calls, agent.Model)
		}
	})

	t.Run("paid fallback is visible before any turn", func(t *testing.T) {
		storeFirstRunKey(t)
		a, _, errOut := newTestApp(t, "")
		a.chooseDefault = func(context.Context, *provider.Client) defaultModelChoice {
			return defaultModelChoice{Model: "paid/cheap", Warning: "no free model; charges may apply"}
		}
		if _, err := a.newAgent(context.Background(), &options{}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(errOut.String(), "charges may apply") {
			t.Fatalf("paid fallback warning = %q", errOut.String())
		}
	})

	t.Run("old documented free preset migrates but mixed custom tiers do not", func(t *testing.T) {
		dirs := storeFirstRunKey(t)
		legacyTiers := map[string]string{
			"quick": legacyFreePreset, "standard": legacyFreePreset,
			"deep": legacyFreePreset, "ultra": legacyFreePreset,
		}
		if err := config.Save(dirs.ConfigFile(), &config.Config{Model: legacyFreePreset, Tiers: legacyTiers}); err != nil {
			t.Fatal(err)
		}
		a, _, errOut := newTestApp(t, "")
		a.chooseDefault = func(context.Context, *provider.Client) defaultModelChoice {
			return defaultModelChoice{Model: "free/current-code", Free: true}
		}
		agent, err := a.newAgent(context.Background(), &options{})
		if err != nil {
			t.Fatal(err)
		}
		if agent.Model != "free/current-code" || len(agent.Tiers) != 0 ||
			!strings.Contains(errOut.String(), "no longer guaranteed free") {
			t.Fatalf("legacy migration model/tiers/stderr = %q/%v/%q", agent.Model, agent.Tiers, errOut.String())
		}

		mixed := &config.Config{Model: legacyFreePreset, Tiers: map[string]string{"quick": "user/custom"}}
		if retireLegacyFreeConfig(mixed) || mixed.Model != legacyFreePreset || mixed.Tiers["quick"] != "user/custom" {
			t.Fatalf("mixed custom routing was changed: %#v", mixed)
		}
	})
}
