package provider

import "strings"

// PlanModel describes a model and the effort levels exposed by a provider
// subscription CLI. It is metadata only; availability is not authentication.
type PlanModel struct {
	Provider  string
	Plan      string
	Connector string
	Model     string
	Efforts   []string
}

var planModelCatalog = []PlanModel{
	{Provider: "anthropic", Plan: "Claude Pro", Connector: "claude", Model: "claude-sonnet", Efforts: []string{"low", "medium", "high"}},
	{Provider: "anthropic", Plan: "Claude Max", Connector: "claude", Model: "claude-opus", Efforts: []string{"low", "medium", "high", "max"}},
	{Provider: "openai", Plan: "ChatGPT Plus", Connector: "codex", Model: "gpt-4.1", Efforts: []string{"low", "medium", "high"}},
	{Provider: "openai", Plan: "ChatGPT Pro", Connector: "codex", Model: "o3", Efforts: []string{"low", "medium", "high", "max"}},
	{Provider: "google", Plan: "Google AI Pro", Connector: "gemini", Model: "gemini-2.5-pro", Efforts: []string{"low", "medium", "high"}},
	{Provider: "google", Plan: "Google AI Pro", Connector: "gemini", Model: "gemini-2.5-flash", Efforts: []string{"low", "medium", "high"}},
	{Provider: "google", Plan: "Google AI Ultra", Connector: "gemini", Model: "gemini-2.5-pro", Efforts: []string{"low", "medium", "high", "max"}},
}

func PlanModels(filter string) []PlanModel {
	filter = strings.ToLower(strings.TrimSpace(filter))
	out := make([]PlanModel, 0, len(planModelCatalog))
	for _, model := range planModelCatalog {
		if filter != "" &&
			!strings.Contains(strings.ToLower(model.Provider), filter) &&
			!strings.Contains(strings.ToLower(model.Plan), filter) &&
			!strings.Contains(strings.ToLower(model.Connector), filter) &&
			!strings.Contains(strings.ToLower(model.Model), filter) {
			continue
		}
		out = append(out, model)
	}
	return out
}
