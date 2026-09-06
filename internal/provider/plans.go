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
	// The documented path to Gemini: an API key on the OpenAI-compatible
	// endpoint (plan 24, read 2026-09-05). The subscription rows above stay
	// unsupported metadata; this row is the one kolk can use.
	{Provider: "google", Name: "Gemini API", Connector: "gemini-api", Auth: "API key", Billing: "metered", Sandbox: false},
	{Provider: "xai", Name: "Grok", Connector: "xai-api", Auth: "API key", Billing: "metered", Sandbox: false},
	{Provider: "perplexity", Name: "Perplexity Pro", Connector: "perplexity-api", Auth: "API key", Billing: "metered", Sandbox: false},
	{Provider: "mistral", Name: "Le Chat Pro", Connector: "mistral-api", Auth: "API key", Billing: "metered", Sandbox: false},
	{Provider: "deepseek", Name: "DeepSeek", Connector: "deepseek-api", Auth: "API key", Billing: "metered", Sandbox: false},
	{Provider: "qwen", Name: "Qwen", Connector: "qwen-api", Auth: "API key", Billing: "metered", Sandbox: false},
	{Provider: "github", Name: "Copilot Pro", Connector: "copilot", Auth: "provider CLI", Billing: "subscription", Sandbox: true},
	{Provider: "cohere", Name: "Cohere Developer", Connector: "cohere-api", Auth: "API key", Billing: "metered", Sandbox: false},
	// Ollama's paid ollama.com tier, which is not the same thing as the local
	// Ollama this machine may already be running — `kolk localia` owns that.
	// Sandbox is false where every other provider-CLI row is true: sandbox
	// means the vendor's CLI enforces its own tool-execution jail, which is
	// what `claude --permission-mode` and `codex --sandbox` do. `ollama run`
	// has no such flag because ollama runs inference, not an agent, and
	// claiming "yes" in that column would describe a jail that does not exist.
	{Provider: "ollama", Name: "Ollama Pro", Connector: "ollama", Auth: "provider CLI", Billing: "subscription", Sandbox: false},
}

// Plans returns all known plans matching filter across provider, plan,
// connector, authentication, and billing fields.
func Plans(filter string) []Plan {
	out := make([]Plan, 0, len(planCatalog))
	for _, plan := range planCatalog {
		row := strings.Join([]string{
			plan.Provider, plan.Name, plan.Connector, plan.Auth, plan.Billing,
		}, " ")
		if !matchesEveryWord(row, filter) {
			continue
		}
		out = append(out, plan)
	}
	return out
}

// matchesEveryWord reports whether every word of filter appears somewhere in
// row, in any order.
//
// The fields used to be matched one at a time against the whole filter, so a
// search had to name a single field and name it in order: `kolk plans claude
// max` worked only because "claude max" happens to be the plan's name, and
// `kolk plans max claude` or `kolk plans anthropic max` — a provider and a
// tier, which is how people describe a plan — found nothing at all.
func matchesEveryWord(row, filter string) bool {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return true
	}
	row = strings.ToLower(row)
	for _, word := range strings.Fields(strings.ToLower(filter)) {
		if !strings.Contains(row, word) {
			return false
		}
	}
	return true
}
