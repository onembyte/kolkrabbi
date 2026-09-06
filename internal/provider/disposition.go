package provider

import "strings"

// Disposition is plan 24's per-provider record: how kolk may reach a provider,
// what that costs, and the evidence read on a date. It is data so a gate can
// hold it: no model row claims a provider-CLI path for a provider that is not
// shipped, and every provider kolk names says where it stands (V34.4c.0).
type Disposition struct {
	Provider string
	// Status is shipped, chosen (owner picked it; engineering queued),
	// investigating (owner picked it; the access path is not yet one kolk
	// speaks), or deferred (owner said later).
	Status string
	// AccessPath is the shape plan 24 permits for this provider today.
	AccessPath string
	// APIBase is the OpenAI-compatible base for an API-key path; empty for a
	// handover or a provider with no documented API path.
	APIBase string
	// KeyEnv and KeyShape name the provider's own key variable and the prefix
	// redaction already knows (internal/redact/keyshapes.json).
	KeyEnv   string
	KeyShape string
	// Billing is subscription, API metered, or unknown — never an estimate
	// dressed as the provider's own figure.
	Billing string
	// Capabilities the fetched documentation stated; absent means unstated,
	// not absent.
	Capabilities Capabilities
	// Terms is what was read of the provider's terms and when; unfetchable is
	// said as such.
	Terms string
	// Evidence lists the pages read and their dates.
	Evidence []string
	// Blockers is what stands between this row and shipped.
	Blockers []string
}

// Capabilities are the facts a chat backend needs before it can be enabled.
type Capabilities struct {
	ChatCompletions bool
	Streaming       bool
	Tools           bool
	ModelsList      bool
	ReasoningVocab  []string
	// ReasoningByRung projects kolk's five rungs onto the vendor's words; a
	// rung absent here sends nothing.
	ReasoningByRung map[string]string
}

const (
	dispositionShipped       = "shipped"
	dispositionChosen        = "chosen"
	dispositionInvestigating = "investigating"
	dispositionDeferred      = "deferred"
)

// Read on 2026-09-05 through read-only fetches; the dates are the pages' own
// where they showed one, the fetch date otherwise.
var dispositions = []Disposition{
	{
		Provider: "anthropic", Status: dispositionShipped,
		AccessPath: "provider CLI handover (claude)", Billing: "subscription",
		Terms:    "handover launches the vendor's own client unmodified; nothing of its login is read",
		Evidence: []string{"plan 24 rows verified live 2026-09-02, claude 2.1.258"},
	},
	{
		Provider: "openai", Status: dispositionShipped,
		AccessPath: "provider CLI handover (codex)", Billing: "subscription",
		Terms:    "handover launches the vendor's own client unmodified; nothing of its login is read",
		Evidence: []string{"plan 24 rows verified live 2026-09-02, codex 0.149.1"},
	},
	{
		Provider: "google", Status: dispositionChosen,
		AccessPath: "API key on the Gemini OpenAI-compatible endpoint; Gemini CLI OAuth is not reused (plan 24)",
		APIBase:    "https://generativelanguage.googleapis.com/v1beta/openai/",
		KeyEnv:     "GEMINI_API_KEY", KeyShape: "AIza",
		Billing: "API metered (a paid key); the free tier is billed nothing and trains on inputs",
		Capabilities: Capabilities{ChatCompletions: true, Streaming: true, Tools: true, ModelsList: true,
			ReasoningVocab:  []string{"reasoning_effort → thinking_level / thinking_budget"},
			ReasoningByRung: map[string]string{"low": "low", "medium": "medium", "high": "high", "max": "high", "ultra": "high"}},
		Terms: "Gemini API additional terms, effective 2026-03-23: 'for developers building with Google AI models for professional or business purposes, not for consumer use'; unpaid tier: 'Do not submit sensitive, confidential, or personal information'; Google AI Pro/Ultra subscriptions are not said to grant API access",
		Evidence: []string{
			"https://ai.google.dev/gemini-api/docs/openai (last updated 2026-09-02): Bearer key, /models, streaming, function calling, 'still in beta while we extend feature support'",
			"https://ai.google.dev/gemini-api/terms (effective 2026-03-23)",
		},
		Blockers: []string{
			"the free tier trains on inputs: kolk must say so before a free Gemini key answers (V34.4c.1b)",
			"the row flip (V34.4c.1b.ii)",
			"the consumer-use clause: kolk is a developer tool, and the row must say what the terms say",
		},
	},
	{
		Provider: "xai", Status: dispositionChosen,
		AccessPath: "API key on the xAI OpenAI-compatible endpoint",
		APIBase:    "https://api.x.ai/v1",
		KeyEnv:     "XAI_API_KEY", KeyShape: "xai-",
		Billing: "API metered, per million tokens; consumer Grok subscriptions are not documented to grant API access",
		Capabilities: Capabilities{ChatCompletions: true, Streaming: true, Tools: true, ModelsList: false,
			ReasoningVocab:  []string{"none", "low", "medium", "high", "xhigh"},
			ReasoningByRung: map[string]string{"low": "low", "medium": "medium", "high": "high", "max": "xhigh", "ultra": "xhigh"}},
		Terms: "unverified: x.ai/legal terms pages refused the read-only fetch (HTTP 403) on 2026-09-05; the owner confirms before the row ships",
		Evidence: []string{
			"https://docs.x.ai/docs/overview (2026-09-05): base https://api.x.ai/v1, Bearer XAI_API_KEY, function calling",
			"https://docs.x.ai/docs/api-reference (2026-09-05): POST /v1/chat/completions, reasoning_effort none|low|medium|high|xhigh, stream SSE, tools with parallel_tool_calls; no models listing documented on the pages read",
			"https://docs.x.ai/docs/models (2026-09-05): grok-4.6, grok-4.5, grok-4.3, grok-4.20-*; prices per million tokens",
		},
		Blockers: []string{
			"terms not readable by tool; owner confirmation",
			"a models listing must be probed live before discovery relies on it; the row flip (V34.4c.1b.ii)",
		},
	},
	{
		Provider: "perplexity", Status: dispositionInvestigating,
		AccessPath: "API key; the chat-completions endpoint is deprecated and the Agent API is Responses-shaped, which kolk does not speak",
		APIBase:    "https://api.perplexity.ai",
		KeyEnv:     "PERPLEXITY_API_KEY", KeyShape: "pplx-",
		Billing:      "API metered, 'metered from each response's usage field'; Pro/Max subscriber credits not documented on the pages read",
		Capabilities: Capabilities{ChatCompletions: false, Streaming: true, Tools: true, ModelsList: false},
		Terms:        "unverified: perplexity.ai/hub/legal API terms refused the read-only fetch (HTTP 403) on 2026-09-05",
		Evidence: []string{
			"https://docs.perplexity.ai/api-reference/chat-completions-post (2026-09-05): POST /v1/sonar, 'Sonar Chat Completions is now Agent API. Sonar will be supported until September 27, 2026'",
			"https://docs.perplexity.ai/docs/agent-api/quickstart (2026-09-05): POST /v1/agent, alias POST /v1/responses, Bearer PERPLEXITY_API_KEY, models such as openai/gpt-5.6-sol",
			"https://docs.perplexity.ai/getting-started/pricing (2026-09-05): token-based, no per-request fee on the Router API",
			"https://docs.perplexity.ai/getting-started/overview (2026-09-05): a Router API described as OpenAI-compatible; its base URL was not on any page read (guides paths returned 404)",
		},
		Blockers: []string{
			"a Responses-shaped API needs a translator kolk does not have, or the Router API's chat-completions base confirmed — an owner decision on which, if either",
			"terms not readable by tool",
		},
	},
	{
		Provider: "github", Status: dispositionChosen,
		AccessPath:   "provider CLI handover (Copilot CLI); GitHub Models — the API path — was retired 2026-07-30",
		Billing:      "subscription; the CLI reports 'GitHub AI Credits used' per session",
		Capabilities: Capabilities{ChatCompletions: false, Streaming: false, Tools: true, ModelsList: false},
		Terms:        "GitHub Terms of Service §J AI Features (effective 2026-04-27): no clause found restricting Copilot to GitHub-provided clients; inputs may train models unless opted out in account settings; Business/Enterprise are under the Copilot Product Specific Terms, not read",
		Evidence: []string{
			"https://docs.github.com/en/github-models/use-github-models/prototyping-with-ai-models (2026-09-05): 'GitHub Models has been fully retired' as of 2026-07-30 — inference API and BYOK gone",
			"https://docs.github.com/en/copilot/how-tos/set-up/install-copilot-cli (2026-09-05): npm install -g @github/copilot; brew install --cask copilot-cli; winget install GitHub.Copilot; auth by /login or a fine-grained PAT with 'Copilot Requests' via COPILOT_GITHUB_TOKEN, GH_TOKEN, GITHUB_TOKEN",
			"https://docs.github.com/en/copilot/reference/copilot-cli-reference/cli-programmatic-reference (2026-09-05): -p PROMPT non-interactive, -s response only, --allow-all-tools, --allow-tool, --deny-tool, --model (gpt-5.4, claude-haiku-4.5, gpt-5.3-codex), --add-dir; /model lists models",
			"https://docs.github.com/en/site-policy/github-terms/github-terms-of-service §J (effective 2026-04-27)",
		},
		Blockers: []string{
			"a Copilot handover (agentcli) with the -p/-s contract, verified-by-turn model truth, cancellation and redaction — V34.4c.2",
			"which Copilot plans include the CLI is not enumerated on the pages read; the seed row stays Copilot Pro until a login says otherwise",
		},
	},
	{Provider: "mistral", Status: dispositionDeferred, AccessPath: "API key (deferred)", Billing: "unknown", Evidence: []string{"owner decision 2026-09-05: deferred"}},
	{Provider: "deepseek", Status: dispositionDeferred, AccessPath: "API key (deferred)", Billing: "unknown", Evidence: []string{"owner decision 2026-09-05: deferred"}},
	{Provider: "qwen", Status: dispositionDeferred, AccessPath: "API key (deferred)", Billing: "unknown", Evidence: []string{"owner decision 2026-09-05: deferred"}},
	{Provider: "cohere", Status: dispositionDeferred, AccessPath: "API key (deferred)", Billing: "unknown", Evidence: []string{"owner decision 2026-09-05: deferred"}},
	{Provider: "ollama", Status: dispositionDeferred, AccessPath: "ollama.com paid tier (deferred); the local server is `kolk localia`", Billing: "unknown", Evidence: []string{"plan 24: not in the v1 checklist"}},
}

func dispositionFor(provider string) (Disposition, bool) {
	wanted := strings.ToLower(strings.TrimSpace(provider))
	for _, d := range dispositions {
		if d.Provider == wanted {
			return d, true
		}
	}
	return Disposition{}, false
}
