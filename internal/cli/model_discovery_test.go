package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/local"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

// The port contract, enforced: a connector without a lister cannot exist.
// Every plan kolk can name resolves to a lister, and every lister either
// answers or says why it cannot — never nil, never an empty success.
func TestEveryConnectorCanListItsModels(t *testing.T) {
	gateway := []provider.ModelInfo{
		{ID: "anthropic/claude-fable-5", Name: "Claude Fable 5", ContextLength: 1000000},
		{ID: "google/gemini-2.5-pro", Name: "Gemini 2.5 Pro", ContextLength: 1000000},
		{ID: "x-ai/grok-5", Name: "Grok 5", ContextLength: 256000},
		{ID: "perplexity/sonar-pro", Name: "Sonar Pro", ContextLength: 200000},
		{ID: "mistralai/mistral-large", Name: "Mistral Large", ContextLength: 128000},
		{ID: "deepseek/deepseek-r1", Name: "DeepSeek R1", ContextLength: 128000},
		{ID: "qwen/qwen3-coder", Name: "Qwen3 Coder", ContextLength: 256000},
		{ID: "cohere/command-a", Name: "Command A", ContextLength: 256000},
	}
	seen := map[string]bool{}
	for _, plan := range provider.Plans("") {
		if seen[plan.Connector] {
			continue
		}
		seen[plan.Connector] = true
		lister := modelListerFor(plan.Connector, gateway)
		if lister == nil {
			t.Errorf("connector %q (%s) has no model lister; a vendor without one cannot be registered", plan.Connector, plan.Name)
			continue
		}
		switch l := lister.(type) {
		case provider.NotListable:
			if l.Reason == "" {
				t.Errorf("connector %q is NotListable without a reason", plan.Connector)
			}
		case provider.GatewayPreviewLister:
			catalog, err := l.Discover(context.Background())
			if err != nil {
				t.Errorf("connector %q gateway preview: %v", plan.Connector, err)
				continue
			}
			for _, model := range catalog.Models {
				if model.Status != provider.StatusUnverified {
					t.Errorf("connector %q previewed %s as %q, want unverified before any turn", plan.Connector, model.ID, model.Status)
				}
			}
		}
	}
	if modelListerFor("no-such-vendor", gateway) != nil {
		t.Fatal("an unknown connector produced a lister")
	}
}

func TestClaudePreviewCarriesTheVendorEffortSet(t *testing.T) {
	gateway := []provider.ModelInfo{{ID: "anthropic/claude-sonnet-5", Name: "Claude Sonnet 5", ContextLength: 1000000}}
	catalog, err := modelListerFor("claude", gateway).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Models) != 1 || strings.Join(catalog.Models[0].Efforts, ",") != "low,medium,high,xhigh,max" {
		t.Fatalf("claude preview = %+v, want the CLI's five efforts on the exact gateway id", catalog.Models)
	}
}

func TestOllamaListerAsksTheCloudCatalogAndReportsFailure(t *testing.T) {
	lister := ollamaCloudLister{list: func(context.Context) ([]local.CloudCatalogModel, error) {
		return []local.CloudCatalogModel{{Name: "qwen3-coder:480b-cloud", Parameters: "480B"}}, nil
	}}
	catalog, err := lister.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Models) != 1 || catalog.Models[0].ID != "qwen3-coder:480b-cloud" || catalog.Models[0].Status != provider.StatusListed {
		t.Fatalf("ollama catalog = %+v", catalog.Models)
	}
	down := ollamaCloudLister{list: func(context.Context) ([]local.CloudCatalogModel, error) { return nil, errors.New("dial tcp: timeout") }}
	if _, err := down.Discover(context.Background()); err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("unreachable cloud catalog = %v, want the reason", err)
	}
}
