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
			ID:            "generic/chat:free",
			Name:          "Generic Chat Free",
			Description:   "A general purpose conversational assistant",
			ContextLength: 32000,
		},
		{
			ID:            "coding/expert:free",
			Name:          "Expert Code LLM Free",
			Description:   "Specialized in programming, software engineering and coding agents",
			ContextLength: 131072,
		},
		{
			ID:            "openrouter/auto",
			Name:          "OpenRouter Auto",
			ContextLength: 128000,
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
