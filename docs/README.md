# kolkrabbi documentation

Start at the [README](../README.md) if you want to *use* kolk. This directory is how it was
decided and built.

## Map

| Directory | What it is |
|---|---|
| [`plan/`](plan/) | **Settled design decisions.** One file per [`PLAN.md`](../PLAN.md) item: the decision, the spec, the alternatives rejected, and the risks. These are the source of truth for how kolk behaves. |
| [`research/`](research/) | **Dated snapshots** of the research behind those decisions — vendor policies, API limits, competitor surveys, platform constraints. Verify before relying on a specific number; they age. |
| [`build-log.md`](build-log.md) | The running implementation log. |
| [`cloudflare-pages.md`](cloudflare-pages.md) | How the site is deployed. |

## The decisions, by area

**Foundations** — [02 architecture](plan/02-architecture.md) ·
[03 provider layer](plan/03-provider-layer.md) ·
[04 subscription backends](plan/04-subscription-backends.md) ·
[05 auth, keys & secrets](plan/05-auth-keys-secrets.md) ·
[18 config](plan/18-config.md)

**Core UX** — [06 modes](plan/06-modes.md) · [07 effort dial](plan/07-effort-dial.md) ·
[08 model routing](plan/08-model-routing.md) ·
[09 command surface](plan/09-command-surface.md) · [10 saga loop](plan/10-saga-loop.md) ·
[11 REPL & TUI](plan/11-repl-tui-input.md) ·
[12 sessions, context, memory](plan/12-sessions-context-memory.md) ·
[13 tools & permissions](plan/13-tools-permissions-sandboxing.md) ·
[14 orchestration](plan/14-orchestration-routing.md) · [15 code mode](plan/15-code-mode.md)

**Data & platform** — [17 local dashboard](plan/17-local-dashboard.md) ·
[24 provider matrix](plan/24-subscription-provider-matrix.md) ·
[25 managed local models](plan/25-managed-local-models.md) ·
[26 remote access](plan/26-remote-access.md)

## How to read these

Each `plan/NN-slug.md` follows one template ([`plan/README.md`](plan/README.md)): **Decision** first
for the short version, **Spec** for what to implement, then **Alternatives rejected** and
**Risks & open questions** — the last of which is where anything still genuinely undecided lives.

Two conventions worth knowing:

- **Claims are marked when unverified.** Where something could not be tested (no API key, a vendor
  behaviour only observable in production), the doc says so at the point of use rather than
  smoothing it over.
- **Mechanisms beat promises.** Where a doc says a guarantee is enforced, it names the test, the
  type, or the CI check that enforces it.

The North star at the top of [`PLAN.md`](../PLAN.md) outranks every one of these documents: zero
config, one install command, one key command.
