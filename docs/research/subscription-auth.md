# Research: subscription-backed backends (Claude Max, ChatGPT, Google login)

Date: 2026-08-22. Method: WebFetch of vendor primary docs + `gh api` for repo docs. The session's
web-search budget was exhausted, so January-2026 news coverage of the OpenCode enforcement was
NOT independently fetched — but the vendor documents below are current, explicit, and sufficient.
Feeds PLAN.md item 4. **Policies change: re-verify these pages before implementing.**

## 1. Anthropic — the controlling language (all fetched 2026-08-22)

From **Claude Code "Legal and compliance"** (https://code.claude.com/docs/en/legal-and-compliance):

> "**OAuth authentication** is intended exclusively for purchasers of Claude Free, Pro, Max,
> Team, and Enterprise subscription plans and is designed to support ordinary use of Claude Code
> and other native Anthropic applications."

> "**Developers** building products or services that interact with Claude's capabilities,
> including those using the Agent SDK, should use API key authentication […] Anthropic does not
> permit third-party developers to offer Claude.ai login into their own applications, or to
> route requests through Free, Pro, or Max plan credentials on behalf of their users. Moreover,
> developers may not collect, store, or intermediate Claude.ai credentials or session tokens —
> sign-in to a Claude account must complete through Anthropic's own flow."

> "Nor does it prevent **an end user from signing in to the unmodified Claude Code binary with
> their own Claude subscription**, including where a platform hosts Claude Code as described
> under *Can customers offer Claude Code in their products?* above."

That section's conditions for offering/running Claude Code inside another product: agree to the
Commercial Terms; "**The Claude Code binary must not be modified**" (and no auth method removed);
"**Customers may not pay for, resell, or intermediate Claude usage on their end users' behalf**"
— each end user authenticates with their own API key, **Claude subscription plan credentials**,
or 3P provider credential, billed directly to them. Also: "Advertised usage limits for Pro and
Max plans assume ordinary, individual usage of Claude Code **and the Agent SDK**." And:
"Anthropic reserves the right to take measures to enforce these restrictions and may do so
without prior notice."

From the **Agent SDK overview** (https://code.claude.com/docs/en/agent-sdk/overview):

> "Unless previously approved, Anthropic does not allow third party developers to offer
> claude.ai login or rate limits for their products, including agents built on the Claude Agent
> SDK. Use the API key authentication methods described in the Quickstart instead."

> "The SDK is available as a library for Python and TypeScript only. To drive the same agent
> loop from another language, **run the CLI as a subprocess** with the `-p` flag and
> `--output-format json`." ← the documented integration surface for a Go program.

Branding rules on the same page: a product built on the SDK must NOT be called or styled
"Claude Code"; allowed: "Claude Agent", "{YourAgentName} Powered by Claude".

From **"Run Claude Code programmatically"** (https://code.claude.com/docs/en/headless):
`claude -p` with `--output-format json|stream-json`, `--input-format stream-json`,
`--allowedTools`, `--permission-mode`, `--resume/--continue`, `--append-system-prompt`,
`--json-schema`; JSON output includes `total_cost_usd` + per-model breakdown (client-side
estimate); `system/init` and `system/api_retry` events. Notably: `--bare` "doesn't use your
subscription login" and "never reads OAuth credentials" — i.e. **plain (non-bare) `claude -p`
runs on the user's existing subscription login**, in an officially documented programmatic mode.

From the **Consumer Terms** (https://www.anthropic.com/legal/consumer-terms): no access "through
automated or non-human means" *except* "via an Anthropic API Key or where we otherwise
explicitly permit it" (headless Claude Code is such an explicitly documented mode); no sharing
of account credentials.

Corroboration from **OpenCode's own docs** (https://opencode.ai/docs/providers): "There are
plugins that allow you to use your Claude Pro/Max models with OpenCode. **Anthropic explicitly
prohibits this.**" and bundled plugins were removed "as of 1.3.0". (The same page still shows a
ChatGPT Plus/Pro sign-in flow for OpenAI.)

## 2. OpenAI (Codex) — partially verified

- Codex CLI signs in with ChatGPT ("Sign in with ChatGPT or another available sign-in method")
  or API key; `codex exec` is documented for "repeatable workflows and pipelines"; an
  **app-server** is listed as a "build with Codex" surface
  (https://learn.chatgpt.com/docs/codex/cli). The dedicated auth page returned 404 this session.
- OpenCode publicly ships ChatGPT subscription sign-in (its docs describe it without any
  prohibition warning) — evidence OpenAI currently tolerates or permits third-party harnesses on
  ChatGPT login, unlike Anthropic. **Unverified against OpenAI's actual terms — must be checked
  before building shape (iv).**

## 3. Google (Gemini CLI) — explicit prohibition on OAuth reuse

From `docs/resources/tos-privacy.md` in google-gemini/gemini-cli (fetched via GitHub API):

> "Directly accessing the services powering Gemini CLI (for example, the Gemini Code Assist
> service) using third-party software, tools, or services (**for example, using OpenClaw with
> Gemini CLI OAuth**) is a violation of applicable terms and policies. Such actions may be
> grounds for suspension or termination of your account."

And `docs/get-started/authentication.mdx` routes **headless mode → API key or Vertex**, not the
Google-account login. So: token reuse = prohibited; even wrapping the `gemini` binary headlessly
is steered to API keys.

## 4. Verdict table for kolk's five integration shapes

| # | Shape | Verdict | Confidence |
|---|---|---|---|
| i | Extract Claude Code's OAuth token, call the API directly | **Prohibited** ("may not collect, store, or intermediate … session tokens") | High |
| ii | Claude Agent SDK on a subscription login, offered in kolk | **Prohibited unless previously approved**; API key is the sanctioned path | High |
| iii | Spawn the user's **own, unmodified, already-logged-in** `claude` binary (`claude -p --output-format stream-json`); kolk never touches credentials; login happens in Claude's own flow | **Permitted with conditions** — this is the shape the docs themselves describe (subprocess integration; end users signing in to the unmodified binary with their own subscription). Conditions: binary unmodified, no auth method disabled, no resale/intermediation, branding ≠ "Claude Code", risk note to users | Medium-high |
| iv | Same pattern with `codex exec` / app-server on ChatGPT login | **Gray, leaning allowed in practice** (OpenCode ships it; official programmatic surfaces exist) — verify OpenAI terms first | Low-medium |
| v | Same pattern with `gemini` CLI on Google login | **OAuth reuse prohibited; headless is steered to API keys** → treat as API-key-only backend | Medium-high |

## 5. Recommendation for kolk

1. Model these as **"external agent CLI" backends**, not API providers: kolk spawns the vendor's
   own binary over its documented stream-JSON interface, renders its events, and records usage
   into the local dashboard (`total_cost_usd`, models, durations from result events). The vendor
   CLI runs its own tools in this mode; kolk is frontend + recorder + router.
2. Login UX: `kolk login claude` = check `claude` is installed → if not logged in, **launch the
   vendor CLI so the user signs in through Anthropic's own flow** — kolk never sees, stores, or
   proxies tokens. Same pattern for codex/gemini (gemini: API key only).
3. Naming/branding: call it "Claude Agent (your Claude subscription)" — never "Claude Code mode".
4. Ship the OpenRouter/API-key path as the default first-class citizen; subscription backends
   are optional adapters behind one interface, with a short policy note + "enforcement may occur
   without notice" caveat in docs.
5. Keep the adapter capability matrix honest: no per-token cost from OpenRouter-style usage
   accounting, different tool semantics, no model choice beyond what the vendor CLI exposes.

**Open questions for the item-4 hardening loop:** confirm the OpenAI terms for (iv); decide
whether kolk in a hosted/bundled context (future cloud/desktop images) triggers the "offer
Claude Code in your products" Commercial-ToS conditions; re-verify all quotes at build time.
