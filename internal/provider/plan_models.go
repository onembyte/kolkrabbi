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
	{Provider: "anthropic", Plan: "Claude Pro", Connector: "claude", Model: "claude-sonnet", Efforts: []string{"low", "medium", "high"}, Access: "provider CLI"},
	{Provider: "anthropic", Plan: "Claude Max", Connector: "claude", Model: "claude-opus", Efforts: []string{"low", "medium", "high", "max"}, Access: "provider CLI"},
	// The codex rows carry only ids the vendor's own CLI accepted on a ChatGPT
	// login (verified 2026-08-28, codex-cli 0.149.1): gpt-4.1 errors with
	// "model metadata not found" and gpt-5.3-codex / gpt-5.6-pro are refused by
	// a non-Pro ChatGPT account, so until a Pro login proves otherwise, the
	// Pro row keeps the model OpenAI markets for it even though this machine
	// could not end-to-end verify it.
	{Provider: "openai", Plan: "ChatGPT Plus", Connector: "codex", Model: "gpt-5.6-sol", Efforts: []string{"low", "medium", "high", "xhigh"}, Access: "provider CLI"},
	{Provider: "openai", Plan: "ChatGPT Pro", Connector: "codex", Model: "gpt-5.6-pro", Efforts: []string{"low", "medium", "high", "xhigh"}, Access: "provider CLI"},
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
		if qualifier != "" &&
			strings.ToLower(candidate.Plan) != qualifier &&
			strings.ToLower(candidate.Provider) != qualifier {
			continue
		}
		matches = append(matches, candidate)
	}

	switch len(matches) {
	case 0:
		return PlanModel{}, fmt.Errorf("%w: no plan model matches %q; `kolk pmodels` lists them", ErrNotAPlanModel, ref)
	case 1:
	default:
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

	selected := matches[0]
	if selected.Access != "provider CLI" {
		return PlanModel{}, fmt.Errorf("%s on %s is %s, so Kolkrabbi cannot use it",
			selected.Model, selected.Plan, selected.Access)
	}
	for _, connector := range manifest.Connectors {
		if connector.Provider == selected.Provider && connector.Name == selected.Connector && connector.Enabled {
			return selected, nil
		}
	}
	return PlanModel{}, fmt.Errorf("%s needs the %s connector; sign in with: kolk plans login %s %q",
		selected.Model, selected.Connector, selected.Provider, selected.Plan)
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
		for i, name := range planEffortOrder {
			if strings.EqualFold(name, level) {
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
		if strings.EqualFold(level, effort) {
			return effort, false
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
