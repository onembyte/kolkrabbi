# Contributing to kolkrabbi

Thanks for looking. kolkrabbi is a solo project with strong opinions, so the fastest path to a
merged change is a short issue first — especially for anything that adds a dependency, a command,
or a config key.

## The one rule that catches everyone

**Run `./scripts/test.sh`, never a bare `go test ./...`.**

The repo has nested modules. A bare `go test ./...` in the root cannot see them: it prints `ok`
and exits `0` while a nested module's tests never run. `scripts/test.sh` enumerates every
`go.mod`. `make test` does the same thing.

If your editor's cross-module navigation breaks, run `./scripts/dev-workspace.sh` — it writes a
**gitignored** `go.work` so gopls sees all modules as one build. Never commit `go.work`, and never
add a `replace` to the root `go.mod`; it breaks `go install …@latest`.

## Getting set up

```bash
git clone https://github.com/onembyte/kolkrabbi
cd kolkrabbi
make build          # Go 1.25+, CGO_ENABLED=0
./scripts/test.sh   # fully offline: no API key, no network, no cost
```

Tests run against a scripted in-process mock of the provider API (`internal/enginetest`), so the
whole suite works with no key and costs nothing.

## Before you open a PR

`make check` runs what CI runs. Individually:

| Command | What it protects |
|---|---|
| `make test` | every module's tests |
| `make vet` / `make lint` | `go vet`, golangci-lint |
| `make fmt-check` | formatting |
| `make arch` | the layering rules — an upward import fails the build |
| `make purity` | the engine stays stdlib-only and OS-free |
| `make buildtags` | every `_unix.go`/`_windows.go` has its build tag |
| `make budgets` | binary size and cold-start budgets |
| `make spec` | a `spec/**` change also updates `spec/CHANGELOG.md` |

These are mechanical on purpose. The architecture is enforced by tests, not by review
([`docs/plan/02-architecture.md`](docs/plan/02-architecture.md) §5).

## Architecture, in one paragraph

One Go module. `spec/` is the language-neutral contract and `protocol/` is its Go binding; those
are the only things that can break a client. Everything else is `internal/`, arranged in layers —
platform, contract, bus, domain, engine, adapters, surfaces — and imports only ever point
*downward*. The CLI is one client of the engine, not the program itself, which is why a desktop or
mobile client can be added as a directory rather than a rewrite.

Read [`docs/plan/02-architecture.md`](docs/plan/02-architecture.md) before moving code between
packages. Design decisions live in [`docs/plan/`](docs/plan/); each file is the settled answer for
one item of [`PLAN.md`](PLAN.md), and the dated research behind them is kept unpublished
(see [the note in `docs/plan/README.md`](docs/plan/README.md#a-note-on-docsresearch)) — nothing
load-bearing lives only there.

## Commits

Conventional-commit prefixes (`feat:`, `fix:`, `docs:`, `test:`, `refactor:`, `chore:`), a subject
that says what changed for a *user*, and a body explaining why when the reason isn't obvious.
`CHANGELOG.md` is generated from `feat:` and `fix:` subjects, so write them as user-facing lines.

## Security

Do not open a public issue for a vulnerability — see [`SECURITY.md`](SECURITY.md).
