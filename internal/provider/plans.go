package provider

import "strings"

// Plan describes a provider plan without containing credentials or session
// state. It is safe to display and persist as connector metadata.
type Plan struct {
	Provider  string
	Name      string
	Connector string
	Auth      string
	Billing   string
	Sandbox   bool
}

var planCatalog = []Plan{
	{Provider: "anthropic", Name: "Claude Free", Connector: "claude", Auth: "provider CLI", Billing: "subscription", Sandbox: true},
	{Provider: "anthropic", Name: "Claude Pro", Connector: "claude", Auth: "provider CLI", Billing: "subscription", Sandbox: true},
	{Provider: "anthropic", Name: "Claude Max", Connector: "claude", Auth: "provider CLI", Billing: "subscription", Sandbox: true},
	{Provider: "openai", Name: "ChatGPT Plus", Connector: "codex", Auth: "provider CLI", Billing: "subscription", Sandbox: true},
	{Provider: "openai", Name: "ChatGPT Pro", Connector: "codex", Auth: "provider CLI", Billing: "subscription", Sandbox: true},
	{Provider: "google", Name: "Gemini Free", Connector: "gemini", Auth: "provider CLI", Billing: "subscription", Sandbox: true},
	{Provider: "google", Name: "Google AI Pro", Connector: "gemini", Auth: "provider CLI", Billing: "subscription", Sandbox: true},
	{Provider: "google", Name: "Google AI Ultra", Connector: "gemini", Auth: "provider CLI", Billing: "subscription", Sandbox: true},
	{Provider: "xai", Name: "Grok", Connector: "xai-api", Auth: "API key", Billing: "metered", Sandbox: false},
	{Provider: "perplexity", Name: "Perplexity Pro", Connector: "perplexity-api", Auth: "API key", Billing: "metered", Sandbox: false},
	{Provider: "mistral", Name: "Le Chat Pro", Connector: "mistral-api", Auth: "API key", Billing: "metered", Sandbox: false},
	{Provider: "deepseek", Name: "DeepSeek", Connector: "deepseek-api", Auth: "API key", Billing: "metered", Sandbox: false},
	{Provider: "qwen", Name: "Qwen", Connector: "qwen-api", Auth: "API key", Billing: "metered", Sandbox: false},
	{Provider: "github", Name: "Copilot Pro", Connector: "copilot", Auth: "provider CLI", Billing: "subscription", Sandbox: true},
	{Provider: "cohere", Name: "Cohere Developer", Connector: "cohere-api", Auth: "API key", Billing: "metered", Sandbox: false},
}

// Plans returns all known plans matching filter across provider, plan,
// connector, authentication, and billing fields.
func Plans(filter string) []Plan {
	filter = strings.ToLower(strings.TrimSpace(filter))
	out := make([]Plan, 0, len(planCatalog))
	for _, plan := range planCatalog {
		if filter != "" &&
			!strings.Contains(strings.ToLower(plan.Provider), filter) &&
			!strings.Contains(strings.ToLower(plan.Name), filter) &&
			!strings.Contains(strings.ToLower(plan.Connector), filter) &&
			!strings.Contains(strings.ToLower(plan.Auth), filter) &&
			!strings.Contains(strings.ToLower(plan.Billing), filter) {
			continue
		}
		out = append(out, plan)
	}
	return out
}
