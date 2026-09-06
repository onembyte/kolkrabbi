# Subscription provider matrix

Status: v1 scope frozen 2026-09-01 · implementation truth remains V34.4 · PLAN.md item 24 · dispositions are data since 2026-09-05 (`internal/provider/disposition.go`, gated: no provider-CLI model row for a provider that is not shipped)

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
| Google | Gemini Free, Google AI Pro, Google AI Ultra, Workspace plans | Gemini API key on the OpenAI-compatible endpoint `generativelanguage.googleapis.com/v1beta/openai/` (Bearer, `/models`, streaming, tools, `reasoning_effort`; beta); do not reuse Gemini CLI OAuth | **chosen** (owner 2026-09-05). Read 2026-09-05: terms effective 2026-03-23 say "for professional or business purposes, not for consumer use" and the unpaid tier trains on inputs; AI Pro/Ultra are not said to grant API access, so the subscription rows stay unsupported metadata. Blocked on the keyed vendor origin (V34.4c.1) |
| xAI | Grok consumer and business plans | xAI API key on `https://api.x.ai/v1` (Bearer, chat completions, SSE streaming, tools with parallel calls, `reasoning_effort` none/low/medium/high/xhigh; per-million-token billing) | **chosen** (owner 2026-09-05). Read 2026-09-05; the terms pages refused a read-only fetch (403) and the owner confirms them; no models listing documented on the pages read. Blocked on V34.4c.1 |
| Perplexity | Pro, Max, Enterprise | API key on `api.perplexity.ai`; chat completions (`/v1/sonar`) is deprecated — "Sonar will be supported until September 27, 2026" — and the Agent API is Responses-shaped (`/v1/agent`, alias `/v1/responses`); a Router API is called OpenAI-compatible but its base was on no page read | **investigating** (owner chose it 2026-09-05). Needs either the Router base confirmed or a Responses translator kolk does not have — an owner decision; terms pages refused a read-only fetch (403) |
| Mistral | Le Chat Free, Pro, Team, Enterprise | Mistral API credentials; investigate any first-party CLI | post-v1 deferred |
| DeepSeek | Chat/web plans | DeepSeek API credentials; consumer account is not assumed to grant API access | post-v1 deferred |
| Qwen / Alibaba | Qwen consumer and Alibaba Cloud plans | Model Studio/API credentials; investigate regional availability | post-v1 deferred |
| GitHub | Copilot Free, Pro, Pro+, Business, Enterprise | User-installed Copilot CLI handover (`npm install -g @github/copilot`; `-p PROMPT`, `-s`, `--allow-tool`/`--deny-tool`, `--model`; auth by `/login` or a fine-grained PAT with "Copilot Requests"); GitHub Models, the API path, was retired 2026-07-30 | **chosen** (owner 2026-09-05). Read 2026-09-05: ToS §J (effective 2026-04-27) has no clause restricting Copilot to GitHub clients; inputs may train unless opted out. Blocked on the handover (V34.4c.2) |
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

## Model discovery (owner decision, 2026-09-02)

No vendor model name is truth because it is in kolk's source. Every connector must supply a way to
list or verify its models; on every start and every `kolk plans login <connector>` kolk runs it,
caches one vendor catalog, and only then shows models in `kolk models`, `/model`, and `pmodels` —
each with its efforts, default, context, tier, vendor version, fetch time, and a status (`listed`,
`verified`, `unverified`, `gone`). The rows in this document and in `planModelCatalog` are seeds:
shown as `unverified` until the vendor confirms them, `gone` when the vendor stops listing them.
Owner's words: "do not burn model names before knowing what's available … this should be like
these for EVERY vendor." Probed the same day: `codex debug models` lists eight models and
`gpt-5.6-pro` — a kolk seed — is not among them, while `gpt-5.5`/`gpt-5.2` and an `ultra` effort
are; Claude Code has no listing command and a valid name can only be confirmed by a turn.
Leaf: F4 of `FABLE_OPTIMIZATION.md`.
