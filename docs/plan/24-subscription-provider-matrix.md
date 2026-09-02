# Subscription provider matrix

Status: v1 scope frozen 2026-09-01 · implementation truth remains V34.4 · PLAN.md item 24

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
| Anthropic | Claude Pro and Max in current v1; other plans require separate proof. Plan catalog rows (verified live 2026-09-02, claude 2.1.258): `claude-haiku` and `claude-sonnet` on Pro (`low,medium,high`), `claude-opus` and `claude-fable` on Max (`low,medium,high,max`); a Max login reaches every rung, a Pro login is told fable needs Max | User-installed `claude` CLI; native API key separately | [~] handover shipped; tier eligibility for the four Claude rungs is tested (F3 of `FABLE_OPTIMIZATION.md`); V34.4a still owns the general tier-matching proof |
| OpenAI | ChatGPT Plus and Pro in current v1; other plans require separate proof | User-installed Codex CLI; OpenAI API separately | [~] handover shipped; V34.4a/b must prove tier and catalog truth |
| Google | Gemini Free, Google AI Pro, Google AI Ultra, Workspace plans | Gemini API key/OAuth where documented; do not reuse Gemini CLI OAuth | post-v1; current subscription rows are unsupported metadata |
| xAI | Grok consumer and business plans | xAI API credentials; investigate any first-party CLI | post-v1 deferred |
| Perplexity | Pro, Max, Enterprise | Perplexity API credentials; investigate any first-party CLI | post-v1 deferred |
| Mistral | Le Chat Free, Pro, Team, Enterprise | Mistral API credentials; investigate any first-party CLI | post-v1 deferred |
| DeepSeek | Chat/web plans | DeepSeek API credentials; consumer account is not assumed to grant API access | post-v1 deferred |
| Qwen / Alibaba | Qwen consumer and Alibaba Cloud plans | Model Studio/API credentials; investigate regional availability | post-v1 deferred |
| GitHub | Copilot Free, Pro, Pro+, Business, Enterprise | User-installed GitHub/Copilot-compatible CLI only where permitted; API separately | post-v1 deferred |
| Cohere | Trial, developer, enterprise plans | Cohere API credentials | post-v1 deferred |

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

## Delivery order after the v1 scope freeze

1. Harden the shipped Anthropic and OpenAI handovers under V34.4a/b: signed-in tier eligibility,
   selectable model truth, cancellation, redaction, and current provider capability evidence.
2. Keep Gemini visibly unsupported as a subscription connector; metadata must never become a
   default or imply authentication.
3. Revisit Google, xAI, Perplexity, Mistral, DeepSeek, Qwen, GitHub, and Cohere post-v1 only when an
   owner requests the provider and a documented access path can satisfy the acceptance checklist.
