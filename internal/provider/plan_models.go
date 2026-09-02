package provider

import (
	"errors"
	"fmt"
	"strings"
)

// PlanModel describes a model and the effort levels exposed by a provider
// subscription CLI. It is metadata only; availability is not authentication.
type PlanModel struct {
	Provider  string
	Plan      string
	Connector string
	Model     string
	Efforts   []string
	Access    string
}

var planModelCatalog = []PlanModel{
	// The four Claude rungs. Verified 2026-09-02 against claude 2.1.258 by
	// fire-and-check (docs/plan/04 §model): `--model haiku` and `--model
	// fable` each completed a one-turn `-p` call on a signed-in login, while
	// an invented model returned `[claude-code:unrecognized_model]` with
	// api_error_status 404 and total_cost_usd 0. The CLI's own help lists
	// `--effort (low, medium, high, xhigh, max)`; xhigh is the vendor's
	// spelling of max and is folded, not advertised. Haiku is advertised on
	// Pro — a Max login reaches it through planSupportsModel — and fable on
	// Max, where the vendor sells it.
	{Provider: "anthropic", Plan: "Claude Pro", Connector: "claude", Model: "claude-haiku", Efforts: []string{"low", "medium", "high"}, Access: "provider CLI"},
	{Provider: "anthropic", Plan: "Claude Pro", Connector: "claude", Model: "claude-sonnet", Efforts: []string{"low", "medium", "high"}, Access: "provider CLI"},
	{Provider: "anthropic", Plan: "Claude Max", Connector: "claude", Model: "claude-opus", Efforts: []string{"low", "medium", "high", "max"}, Access: "provider CLI"},
	{Provider: "anthropic", Plan: "Claude Max", Connector: "claude", Model: "claude-fable", Efforts: []string{"low", "medium", "high", "max"}, Access: "provider CLI"},
	// The Codex rows carry the model ids the vendor exposes to ChatGPT plan
	// logins (verified against codex-cli 0.149.1). OpenAI documents the current
	// family as Sol (flagship), Terra (balanced), and Luna (cost-efficient); Plus
	// and Pro can choose all three. The existing Pro-only row is retained as the
	// higher Pro tier. `max` is deliberately not advertised here until the local
	// CLI's accepted effort set is verified separately; the adapter currently
	// accepts low, medium, high, and xhigh.
	{Provider: "openai", Plan: "ChatGPT Plus", Connector: "codex", Model: "gpt-5.6-sol", Efforts: []string{"low", "medium", "high", "xhigh"}, Access: "provider CLI"},
	{Provider: "openai", Plan: "ChatGPT Plus", Connector: "codex", Model: "gpt-5.6-terra", Efforts: []string{"low", "medium", "high", "xhigh"}, Access: "provider CLI"},
	{Provider: "openai", Plan: "ChatGPT Plus", Connector: "codex", Model: "gpt-5.6-luna", Efforts: []string{"low", "medium", "high", "xhigh"}, Access: "provider CLI"},
	{Provider: "openai", Plan: "ChatGPT Pro", Connector: "codex", Model: "gpt-5.6-pro", Efforts: []string{"low", "medium", "high", "xhigh"}, Access: "provider CLI"},
	{Provider: "openai", Plan: "ChatGPT Pro", Connector: "codex", Model: "gpt-5.6-sol", Efforts: []string{"low", "medium", "high", "xhigh"}, Access: "provider CLI"},
	{Provider: "openai", Plan: "ChatGPT Pro", Connector: "codex", Model: "gpt-5.6-terra", Efforts: []string{"low", "medium", "high", "xhigh"}, Access: "provider CLI"},
	{Provider: "openai", Plan: "ChatGPT Pro", Connector: "codex", Model: "gpt-5.6-luna", Efforts: []string{"low", "medium", "high", "xhigh"}, Access: "provider CLI"},
	{Provider: "google", Plan: "Google AI Pro", Connector: "gemini", Model: "gemini-2.5-pro", Efforts: []string{"low", "medium", "high"}, Access: "unsupported subscription"},
	{Provider: "google", Plan: "Google AI Pro", Connector: "gemini", Model: "gemini-2.5-flash", Efforts: []string{"low", "medium", "high"}, Access: "unsupported subscription"},
	{Provider: "google", Plan: "Google AI Ultra", Connector: "gemini", Model: "gemini-2.5-pro", Efforts: []string{"low", "medium", "high", "max"}, Access: "unsupported subscription"},
}

func PlanModels(filter string) []PlanModel {
	filter = strings.ToLower(strings.TrimSpace(filter))
	out := make([]PlanModel, 0, len(planModelCatalog))
	for _, model := range planModelCatalog {
		if filter != "" &&
			!strings.Contains(strings.ToLower(model.Provider), filter) &&
			!strings.Contains(strings.ToLower(model.Plan), filter) &&
			!strings.Contains(strings.ToLower(model.Connector), filter) &&
			!strings.Contains(strings.ToLower(model.Model), filter) &&
			!strings.Contains(strings.ToLower(model.Access), filter) {
			continue
		}
		out = append(out, model)
	}
	return out
}

// ErrNotAPlanModel distinguishes "the user named an ordinary model" from "the
// user named a plan model that cannot be used yet". Only the second is worth
// stopping a session over.
var ErrNotAPlanModel = errors.New("not a plan model")

// ResolvePlanModel finds the plan model a user named and refuses, with the
// reason and the next command, when it cannot be used. A plan model is usable
// only when its plan is reachable through a provider CLI and the user has
// already signed into that connector in a terminal Kolkrabbi does not own.
func ResolvePlanModel(ref string, manifest ConnectorManifest) (PlanModel, error) {
	return resolvePlanModel(planModelCatalog, ref, manifest)
}

func resolvePlanModel(catalog []PlanModel, ref string, manifest ConnectorManifest) (PlanModel, error) {
	wanted := strings.ToLower(strings.TrimSpace(ref))
	if target, ok := subscriptionModelAliases[wanted]; ok {
		wanted = strings.ToLower(target)
	}
	if wanted == "" {
		return PlanModel{}, fmt.Errorf("name a plan model; `kolk pmodels` lists them")
	}
	qualifier, model := "", wanted
	if plan, rest, ok := strings.Cut(wanted, "/"); ok {
		qualifier, model = plan, rest
	}

	matches := make([]PlanModel, 0, 2)
	for _, candidate := range catalog {
		if strings.ToLower(candidate.Model) != model {
			continue
		}
		// A slash-qualified plan is intentionally different from an ordinary
		// provider/model gateway id such as `openai/gpt-5.6-luna`. Only the
		// human plan name qualifies a subscription row; provider prefixes remain
		// ordinary model references.
		if qualifier != "" && strings.ToLower(candidate.Plan) != qualifier {
			continue
		}
		matches = append(matches, candidate)
	}

	var selected PlanModel
	switch len(matches) {
	case 0:
		return PlanModel{}, fmt.Errorf("%w: no plan model matches %q; `kolk pmodels` lists them", ErrNotAPlanModel, ref)
	case 1:
		selected = matches[0]
	default:
		// Prefer the exact enabled plan. Overlapping records from one known tier
		// family are resolved below; unrelated products retain their ambiguity.
		enabled := make([]PlanModel, 0, len(matches))
		exact := make([]PlanModel, 0, len(matches))
		for _, candidate := range matches {
			if candidate.Access != "provider CLI" || !connectorEnabledFor(candidate, manifest) {
				continue
			}
			enabled = append(enabled, candidate)
			if connectorExactFor(candidate, manifest) {
				exact = append(exact, candidate)
			}
		}
		if len(exact) == 1 {
			selected = exact[0]
			break
		}
		if preferred, ok := highestKnownSubscriptionTier(exact); ok {
			selected = preferred
			break
		}
		if len(enabled) == 1 {
			selected = enabled[0]
			break
		}
		if preferred, ok := highestKnownSubscriptionTier(enabled); ok {
			selected = preferred
			break
		}
		// Asking someone to choose between plans that are all unusable wastes a
		// round trip. Give the reason now.
		if access, ok := sharedUnusableAccess(matches); ok {
			return PlanModel{}, fmt.Errorf("%s is %s on every plan that offers it, so Kolkrabbi cannot use it",
				matches[0].Model, access)
		}
		qualified := make([]string, 0, len(matches))
		for _, candidate := range matches {
			qualified = append(qualified, candidate.Plan+"/"+candidate.Model)
		}
		return PlanModel{}, fmt.Errorf("%q is offered by more than one plan; name one of %s",
			ref, strings.Join(qualified, ", "))
	}

	if selected.Access != "provider CLI" {
		return PlanModel{}, fmt.Errorf("%s on %s is %s, so Kolkrabbi cannot use it",
			selected.Model, selected.Plan, selected.Access)
	}
	if connectorEnabledFor(selected, manifest) {
		return selected, nil
	}
	return PlanModel{}, fmt.Errorf("%s needs the %s connector; sign in with: kolk plans login %s %q",
		selected.Model, selected.Connector, selected.Provider, selected.Plan)
}

// highestKnownSubscriptionTier resolves overlapping records only when they
// are tiers of the same provider model through the same connector. This makes
// stale Plus+Pro manifests deterministic without guessing between unrelated
// products that happen to use the same model ID. Every candidate must belong
// to a tier hierarchy we know; otherwise the caller retains the ambiguity.
func highestKnownSubscriptionTier(candidates []PlanModel) (PlanModel, bool) {
	if len(candidates) < 2 {
		return PlanModel{}, false
	}
	first := candidates[0]
	selected := first
	highest := -1
	for _, candidate := range candidates {
		if !strings.EqualFold(candidate.Provider, first.Provider) ||
			!strings.EqualFold(candidate.Connector, first.Connector) ||
			!strings.EqualFold(candidate.Model, first.Model) {
			return PlanModel{}, false
		}
		rank, ok := subscriptionTierRank(candidate.Provider, candidate.Plan)
		if !ok {
			return PlanModel{}, false
		}
		if rank > highest {
			selected = candidate
			highest = rank
		}
	}
	return selected, true
}

func connectorEnabledFor(model PlanModel, manifest ConnectorManifest) bool {
	for _, connector := range manifest.Connectors {
		if connector.Provider == model.Provider && connector.Name == model.Connector &&
			connector.Enabled && planSupportsModel(connector.Plan, model.Plan, model.Provider) {
			return true
		}
	}
	return false
}

func connectorExactFor(model PlanModel, manifest ConnectorManifest) bool {
	for _, connector := range manifest.Connectors {
		if connector.Provider == model.Provider && connector.Plan == model.Plan &&
			connector.Name == model.Connector && connector.Enabled {
			return true
		}
	}
	return false
}

// planSupportsModel keeps the plan matrix honest without pretending that a
// higher subscription tier cannot use a model offered on a lower tier. The
// connector stores the tier the user actually signed into; the catalog row
// stores the tier under which a model is advertised.
func planSupportsModel(connectorPlan, modelPlan, provider string) bool {
	if strings.EqualFold(connectorPlan, modelPlan) {
		return true
	}
	connectorRank, ok := subscriptionTierRank(provider, connectorPlan)
	if !ok {
		return false
	}
	modelRank, ok := subscriptionTierRank(provider, modelPlan)
	return ok && connectorRank >= modelRank
}

func subscriptionTierRank(provider, plan string) (int, bool) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	plan = strings.ToLower(strings.TrimSpace(plan))
	switch provider {
	case "anthropic":
		switch plan {
		case "claude pro":
			return 1, true
		case "claude max":
			return 2, true
		}
	case "openai":
		switch plan {
		case "chatgpt plus":
			return 1, true
		case "chatgpt pro":
			return 2, true
		}
	}
	return 0, false
}

// sharedUnusableAccess reports the one reason every candidate is unusable, when
// they all share it.
func sharedUnusableAccess(matches []PlanModel) (string, bool) {
	access := matches[0].Access
	for _, candidate := range matches {
		if candidate.Access != access || candidate.Access == "provider CLI" {
			return "", false
		}
	}
	return access, true
}

// planEffortOrder is the canonical effort ladder, cheapest first. It mirrors
// the engine's levels without importing them: a plan catalog is domain data and
// cannot depend on the engine that consumes it.
var planEffortOrder = []string{"low", "medium", "high", "max"}

// EffortForPlan maps a session's effort onto the closest level a plan actually
// offers and reports whether it had to move. A plan that advertises nothing is
// left alone, and the result is never more expensive than what was asked for:
// when nothing at or below the request is offered, the cheapest level wins
// rather than the nearest one.
func EffortForPlan(effort string, offered []string) (string, bool) {
	if effort == "" || len(offered) == 0 {
		return effort, false
	}
	rank := func(level string) int {
		// Codex calls the shared maximum capability xhigh. It is a provider
		// spelling, not a fifth rung: translating between it and max must not
		// be reported as a downgrade.
		if strings.EqualFold(strings.TrimSpace(level), "xhigh") {
			level = "max"
		}
		for i, name := range planEffortOrder {
			if strings.EqualFold(name, strings.TrimSpace(level)) {
				return i
			}
		}
		return -1
	}
	wanted := rank(effort)
	best, bestRank := "", -1
	lowest, lowestRank := "", len(planEffortOrder)
	for _, level := range offered {
		at := rank(level)
		if at < 0 {
			continue
		}
		if wanted >= 0 && at == wanted {
			return level, false
		}
		if at < lowestRank {
			lowest, lowestRank = level, at
		}
		if wanted >= 0 && at <= wanted && at > bestRank {
			best, bestRank = level, at
		}
	}
	if best != "" {
		return best, true
	}
	if lowest != "" {
		return lowest, true
	}
	return effort, false
}
