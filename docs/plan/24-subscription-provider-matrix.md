# Subscription provider matrix

Status: checklist opened 2026-08-26 · PLAN.md item 24

Kolkrabbi should make every usable model discoverable, but a consumer
subscription is not automatically an API entitlement. Each provider must be
integrated through the access path that the provider permits:

- **Native API**: the user supplies an API key or supported API OAuth grant.
- **Local agent CLI**: kolk launches the provider's unmodified, locally
  authenticated CLI and never reads its credentials.
- **Unsupported**: the consumer web/app session is not exposed as a generic
  third-party API. Do not scrape cookies, read credential files, capture
  refresh tokens, or replay browser traffic.

## Initial subscription checklist

| Provider | Consumer plans to investigate | Supported kolk shape | Status |
|---|---|---|---|
| Anthropic | Claude Free, Pro, Max, Team, Enterprise | User-installed `claude` CLI; native API key separately | [ ] adapter/login handover |
| OpenAI | ChatGPT Free, Plus, Pro, Business, Enterprise | User-installed Codex CLI if its login/use permits; OpenAI API separately | [ ] terms and CLI adapter |
| Google | Gemini Free, Google AI Pro, Google AI Ultra, Workspace plans | Gemini API key/OAuth where documented; do not reuse Gemini CLI OAuth | [ ] API auth review |
| xAI | Grok consumer and business plans | xAI API credentials; investigate any first-party CLI | [ ] API and CLI review |
| Perplexity | Pro, Max, Enterprise | Perplexity API credentials; investigate any first-party CLI | [ ] API review |
| Mistral | Le Chat Free, Pro, Team, Enterprise | Mistral API credentials; investigate any first-party CLI | [ ] API review |
| DeepSeek | Chat/web plans | DeepSeek API credentials; consumer account is not assumed to grant API access | [ ] API review |
| Qwen / Alibaba | Qwen consumer and Alibaba Cloud plans | Model Studio/API credentials; investigate regional availability | [ ] API review |
| GitHub | Copilot Free, Pro, Pro+, Business, Enterprise | User-installed GitHub/Copilot-compatible CLI only where permitted; API separately | [ ] product/terms review |
| Cohere | Trial, developer, enterprise plans | Cohere API credentials | [ ] native adapter review |

Plan names and entitlements change by country and date. The implementation
must verify current provider documentation before shipping each adapter; this
table is an inventory, not a claim that every listed plan grants API access.

## Per-provider acceptance checklist

- [ ] Confirm the provider's current terms permit a third-party local client.
- [ ] Identify whether subscription usage is available through a documented API
  or only through the provider's own application/CLI.
- [ ] Prefer a first-party CLI handover when it owns the subscription login.
- [ ] Never read browser cookies, vendor credential files, OS keychain entries,
  access tokens, refresh tokens, or session tokens.
- [ ] Never impersonate a vendor client, bypass quotas, or proxy one account
  for multiple users.
- [ ] Record billing mode honestly: subscription usage, API metered usage, or
  unknown. Never label an API-equivalent estimate as provider billing.
- [ ] Add offline fixtures, redaction tests, cancellation tests, and a
  provider-specific capability matrix before enabling the backend.

## Delivery order

1. Anthropic `claude` CLI handover and `kolk login claude`.
2. OpenAI Codex CLI handover only after its current terms are verified.
3. Google native API authentication; no Gemini CLI OAuth reuse.
4. xAI, Perplexity, Mistral, DeepSeek, Qwen, GitHub, and Cohere based on
   documented access paths and first-party tooling.

