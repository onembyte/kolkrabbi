package cli

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

const legacyFreePreset = "stealth/ox-alpha"

// defaultModelChoice records enough policy for startup to be transparent when
// a provider exposes no zero-cost model. The official free router remains the
// fallback whenever the catalog cannot say better.
type defaultModelChoice struct {
	Model string
	Free  bool
	// Refused means no model was chosen and none should be substituted — the
	// `stop` policy. Distinct from an empty Model, which startup otherwise
	// fills in with the free router, because "nothing suitable" and "do not
	// start" are different answers and only one of them may be papered over.
	Refused bool
	Warning string
}

// chooseDefaultModel picks the session's model from the catalog startup already
// loaded. It is pure: the catalog comes from the one startup snapshot (cache
// first, network only when no cache exists), so choosing a model never adds a
// request of its own. Until 1.2.2 this function made a second, uncached
// catalog fetch, and the prompt waited on it.
func chooseDefaultModel(catalog []provider.ModelInfo) defaultModelChoice {
	if len(catalog) == 0 {
		return defaultModelChoice{
			Model: defaultModel, Free: true,
			Warning: "could not load the model catalog; using OpenRouter's free router",
		}
	}
	choice, ok := chooseBestDefaultModel(catalog)
	if !ok {
		return defaultModelChoice{
			Model: defaultModel, Free: true,
			Warning: "the catalog listed no usable model; using OpenRouter's free router",
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

// applyFreeExhausted governs chooseDefaultModel's paid fallthrough (B12.13).
//
// The order never changes — free is always preferred — and the policy decides
// only what happens when there is no free tool-capable model to prefer. It takes
// the choice already made rather than re-deriving it, because startup injects
// its own chooser and calling the real one from here would ignore that seam,
// which is exactly how A33.6 first broke two tests.
//
// The previous behaviour was the `paid` branch for everybody: a first run on a
// catalogue with no free coding model quietly started billing, with a warning
// nobody reads before the first turn. A first run is exactly when someone has
// no idea what anything costs.
func applyFreeExhausted(choice defaultModelChoice, policy string) defaultModelChoice {
	if choice.Free {
		return choice
	}
	switch policy {
	case engine.OnFreeExhaustedPaid:
		return choice
	case engine.OnFreeExhaustedStop:
		return defaultModelChoice{
			Refused: true,
			Warning: "the provider listed no free tool-capable model and routing.on_free_exhausted is `stop`; " +
				"name a model with `-m`, or set routing.on_free_exhausted to free or paid",
		}
	default:
		// Free: the router is free and answers, which is a better first run
		// than a bill. Naming the model that was passed over matters — the
		// difference between the two is the whole reason someone would change
		// this setting.
		return defaultModelChoice{
			Model: defaultModel, Free: true,
			Warning: fmt.Sprintf("the provider listed no free tool-capable model; staying free on %s rather than billing for %s "+
				"(`kolk config set routing.on_free_exhausted paid` allows the swap)", defaultModel, choice.Model),
		}
	}
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
	free        bool
	codingScore int
	cost        float64
	costKnown   bool
}

func chooseBestDefaultModel(models []provider.ModelInfo) (defaultModelChoice, bool) {
	verifiedTools := make([]defaultCandidate, 0, len(models))
	unknownTools := make([]defaultCandidate, 0, len(models))
	for _, model := range models {
		if strings.TrimSpace(model.ID) == "" || model.ID == legacyFreePreset {
			continue
		}
		candidate := defaultCandidate{
			model: model, free: modelIsFree(model), codingScore: codingSuitability(model),
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
	// The catalog is a cache with no meaningful order, so ties resolve on the
	// data alone: a larger window, then the ID, both deterministic.
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
	if left.model.ContextLength != right.model.ContextLength {
		return left.model.ContextLength > right.model.ContextLength
	}
	return left.model.ID < right.model.ID
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
