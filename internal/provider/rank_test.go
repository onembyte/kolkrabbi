package provider_test

import (
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

func TestRankFreeModels(t *testing.T) {
	models := []provider.ModelInfo{
		{
			ID:            "paid/super-coder",
			Name:          "Super Coder",
			ContextLength: 200000,
		},
		{
			ID:                  "generic/chat:free",
			Name:                "Generic Chat Free",
			Description:         "A general purpose conversational assistant",
			ContextLength:       32000,
			SupportedParameters: []string{"tools"},
		},
		{
			ID:                  "coding/expert:free",
			Name:                "Expert Code LLM Free",
			Description:         "Specialized in programming, software engineering and coding agents",
			ContextLength:       131072,
			SupportedParameters: []string{"tools"},
		},
		{
			ID:                  "openrouter/auto",
			Name:                "OpenRouter Auto",
			ContextLength:       128000,
			SupportedParameters: []string{"tools"},
		},
	}

	ranked := provider.RankFreeModels(models)

	// Paid model must not appear in ranked free models
	for _, id := range ranked {
		if id == "paid/super-coder" {
			t.Errorf("paid model appeared in RankFreeModels: %s", id)
		}
	}

	if len(ranked) < 2 {
		t.Fatalf("expected at least 2 ranked free models, got %d", len(ranked))
	}

	// coding/expert:free must rank ahead of generic/chat:free due to codingSuitability
	if ranked[0] != "coding/expert:free" {
		t.Errorf("expected coding/expert:free as #1, got %s", ranked[0])
	}
}

func TestRankFreeModelsAppliesCandidateGates(t *testing.T) {
	models := []provider.ModelInfo{
		{ID: "paid/zero", ContextLength: 128000, SupportedParameters: []string{"tools"}},
		{ID: "free/no-tools:free", ContextLength: 128000},
		{ID: "free/short:free", ContextLength: 16000, SupportedParameters: []string{"tools"}},
		{ID: "free/priced", ContextLength: 128000, SupportedParameters: []string{"tools"},
			Pricing: struct {
				Prompt            string `json:"prompt"`
				Completion        string `json:"completion"`
				Request           string `json:"request"`
				InternalReasoning string `json:"internal_reasoning"`
			}{Prompt: "0.001", Completion: "0.002"}},
		{ID: "free/pricing-zero", ContextLength: 128000, SupportedParameters: []string{"tools"},
			Pricing: struct {
				Prompt            string `json:"prompt"`
				Completion        string `json:"completion"`
				Request           string `json:"request"`
				InternalReasoning string `json:"internal_reasoning"`
			}{Prompt: "0", Completion: "0"}},
	}

	got := provider.RankFreeModels(models)
	if len(got) != 1 || got[0] != "free/pricing-zero" {
		t.Fatalf("ranked candidates = %#v, want only zero-cost tool-capable 32k model", got)
	}
}
