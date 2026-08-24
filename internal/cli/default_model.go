package cli

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

const (
	legacyFreePreset      = "stealth/ox-alpha"
	modelDiscoveryTimeout = 5 * time.Second
)

// defaultModelChoice records enough policy for startup to be transparent when
// a provider exposes no zero-cost model. Model discovery itself never blocks a
// first run: the official free router remains the outage fallback.
type defaultModelChoice struct {
	Model   string
	Free    bool
	Warning string
}

func discoverDefaultModel(ctx context.Context, client *provider.Client) defaultModelChoice {
	discoveryContext, cancel := context.WithTimeout(ctx, modelDiscoveryTimeout)
	defer cancel()
	models, err := client.ListModelsRanked(discoveryContext)
	if err != nil {
		return defaultModelChoice{
			Model: defaultModel, Free: true,
			Warning: "could not rank the live model catalog; using OpenRouter's free router",
		}
	}
	choice, ok := chooseBestDefaultModel(models)
	if !ok {
		return defaultModelChoice{
			Model: defaultModel, Free: true,
			Warning: "the live catalog listed no usable model; using OpenRouter's free router",
		}
	}
	if !choice.Free {
		choice.Warning = fmt.Sprintf(
			"the provider listed no free tool-capable model; using the cheapest available coding model %s (charges may apply)",
			choice.Model,
		)
	}
	return choice
}

// retireLegacyFreeConfig recognizes only the exact preset previously printed
// in the README: stealth/ox-alpha as the base model and as every configured
// effort tier. That model is no longer guaranteed free. Mixed/custom tier maps
// are deliberate user routing and remain untouched.
func retireLegacyFreeConfig(cfg *config.Config) bool {
	if cfg == nil || cfg.Model != legacyFreePreset {
		return false
	}
	for _, model := range cfg.Tiers {
		if model != legacyFreePreset {
			return false
		}
	}
	cfg.Model = ""
	for effort := range cfg.Tiers {
		delete(cfg.Tiers, effort)
	}
	return true
}

type defaultCandidate struct {
	model       provider.ModelInfo
	index       int
	free        bool
	codingScore int
	cost        float64
	costKnown   bool
}

func chooseBestDefaultModel(models []provider.ModelInfo) (defaultModelChoice, bool) {
	verifiedTools := make([]defaultCandidate, 0, len(models))
	unknownTools := make([]defaultCandidate, 0, len(models))
	for index, model := range models {
		if strings.TrimSpace(model.ID) == "" || model.ID == legacyFreePreset {
			continue
		}
		candidate := defaultCandidate{
			model: model, index: index, free: modelIsFree(model), codingScore: codingSuitability(model),
		}
		candidate.cost, candidate.costKnown = estimatedModelCost(model)
		switch toolCapability(model) {
		case 2:
			verifiedTools = append(verifiedTools, candidate)
		case 1:
			unknownTools = append(unknownTools, candidate)
		}
	}

	// Cost is the first invariant: a free model with unknown tool metadata is
	// still safer than a paid model that advertises tools. Explicitly non-tool
	// models were discarded above, so the four buckets are ordered by free,
	// then capability confidence.
	for _, candidates := range [][]defaultCandidate{verifiedTools, unknownTools} {
		if best, ok := bestFreeCandidate(candidates); ok {
			return defaultModelChoice{Model: best.model.ID, Free: true}, true
		}
	}
	for _, candidates := range [][]defaultCandidate{verifiedTools, unknownTools} {
		if best, ok := bestPaidCandidate(candidates); ok {
			return defaultModelChoice{Model: best.model.ID}, true
		}
	}
	return defaultModelChoice{}, false
}

func bestFreeCandidate(candidates []defaultCandidate) (defaultCandidate, bool) {
	var best defaultCandidate
	found := false
	for _, candidate := range candidates {
		if !candidate.free {
			continue
		}
		if !found || betterFreeCandidate(candidate, best) {
			best, found = candidate, true
		}
	}
	return best, found
}

func bestPaidCandidate(candidates []defaultCandidate) (defaultCandidate, bool) {
	var best defaultCandidate
	found := false
	for _, candidate := range candidates {
		if candidate.free {
			continue
		}
		if !found || betterPaidCandidate(candidate, best) {
			best, found = candidate, true
		}
	}
	return best, found
}

func betterFreeCandidate(left, right defaultCandidate) bool {
	if left.codingScore != right.codingScore {
		return left.codingScore > right.codingScore
	}
	// The endpoint is requested in intelligence order, so retain its order
	// before using context and ID as deterministic fallbacks.
	if left.index != right.index {
		return left.index < right.index
	}
	if left.model.ContextLength != right.model.ContextLength {
		return left.model.ContextLength > right.model.ContextLength
	}
	return left.model.ID < right.model.ID
}

func betterPaidCandidate(left, right defaultCandidate) bool {
	if left.costKnown != right.costKnown {
		return left.costKnown
	}
	if left.costKnown && left.cost != right.cost {
		return left.cost < right.cost
	}
	if left.codingScore != right.codingScore {
		return left.codingScore > right.codingScore
	}
	return left.index < right.index
}

func toolCapability(model provider.ModelInfo) int {
	if model.ID == defaultModel {
		return 2
	}
	if len(model.SupportedParameters) == 0 {
		return 1
	}
	for _, parameter := range model.SupportedParameters {
		if parameter == "tools" || parameter == "tool_choice" {
			return 2
		}
	}
	return 0
}

func modelIsFree(model provider.ModelInfo) bool {
	// OpenRouter documents :free as the explicit zero-cost variant. Do not
	// infer the same guarantee from a temporary zero in catalog pricing: the
	// retired stealth alias demonstrated that an alias can resolve elsewhere.
	return model.ID == defaultModel || strings.HasSuffix(model.ID, ":free")
}

func estimatedModelCost(model provider.ModelInfo) (float64, bool) {
	prompt, promptOK := nonNegativePrice(model.Pricing.Prompt)
	completion, completionOK := nonNegativePrice(model.Pricing.Completion)
	if !promptOK || !completionOK {
		return math.Inf(1), false
	}
	request, requestOK := optionalPrice(model.Pricing.Request)
	reasoning, reasoningOK := optionalPrice(model.Pricing.InternalReasoning)
	if !requestOK || !reasoningOK {
		return math.Inf(1), false
	}
	// Compare one million input plus output/reasoning tokens and one request.
	// This is a stable fallback heuristic only; zero-cost models are selected
	// in the earlier branch and never compete against these estimates.
	return request + 1_000_000*(prompt+completion+reasoning), true
}

func nonNegativePrice(value string) (float64, bool) {
	price, err := strconv.ParseFloat(value, 64)
	return price, err == nil && price >= 0 && !math.IsInf(price, 0) && !math.IsNaN(price)
}

func optionalPrice(value string) (float64, bool) {
	if value == "" {
		return 0, true
	}
	return nonNegativePrice(value)
}

func codingSuitability(model provider.ModelInfo) int {
	identity := strings.ToLower(model.ID + " " + model.Name)
	description := strings.ToLower(model.Description)
	score := 0
	for _, signal := range []struct {
		text   string
		weight int
	}{
		{"software engineering", 8},
		{"programming", 7},
		{"coding", 7},
		{"coder", 7},
		{"code", 5},
		{"terminal", 4},
		{"swe-bench", 4},
		{"agentic", 2},
	} {
		if strings.Contains(identity, signal.text) {
			score += signal.weight * 2
		}
		if strings.Contains(description, signal.text) {
			score += signal.weight
		}
	}
	return score
}
