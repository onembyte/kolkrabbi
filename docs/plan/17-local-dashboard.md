# 17. The local dashboard

Status: hardened on 2026-08-26 · supersedes: the SQLite plan in `docs/research/dashboard.md` and
migration leaves A12.2 / A12.5 · PLAN.md item 17

## Decision (the short version)

`stats.jsonl` stays the store. No SQLite, no third dependency, no schema.

That reverses the research recommendation, and it is reversed on a measurement rather than a
preference. A heavy user's entire year — 105,000 records, 28 MiB, generated and run on the
development host on 2026-08-26 — loads and aggregates in **578 ms** through the code that already
exists. A database earns its place by making something possible that was not; here it would add the
third third-party module to a graph whose budget gate fails above two, grow the binary, introduce a
migration, and make a 578 ms operation faster than a human can perceive either way.

`kolk dash` serves an embedded page on loopback that renders its charts as server-side SVG. There is
no JavaScript charting library, because a dashboard that draws on the server has no asset pipeline,
no supply chain, and works with scripting disabled.

## Spec

### 1. Storage — `stats.jsonl`, unchanged

| | Decision |
|---|---|
| Format | Append-only JSONL at `<data>/stats.jsonl`. Unchanged. |
| Record | `stats.Record` as it stands, including `cache_read_tokens` / `cache_creation_tokens` (B12.14). |
| Reading | `stats.Load` then `stats.Aggregate`, in memory, per request. |
| Revisit when | A `kolk stats` run on real data exceeds **2 s**. Then measure again before choosing a store; do not adopt one on the assumption that it must be needed by now. |

**No prompt or response text is stored at all.** The research proposed hashing prompts for privacy;
Kolkrabbi does not record them in the first place, which is the stronger position and needs no
opt-in.

### 2. `kolk dash`

```
kolk dash [--addr 127.0.0.1:0] [--open]
```

- Binds loopback only. A stats server on `0.0.0.0` publishes a record of everything the user has
  worked on, and no flag will make that a good default; a non-loopback address is refused with the
  reason, matching `kolk serve`'s existing rule.
- Prints the URL it bound. Default port 0 means "pick a free one", so a second instance never
  collides with the first.
- `--open` launches the browser through `internal/shell`; without it, nothing is launched.
- Reads the same `stats.jsonl` as `kolk stats`. The two can never disagree, because there is one
  reader.

### 3. The v1 views

Four, each answering a question the recorded data can actually answer:

| View | Question | Fields used |
|---|---|---|
| Model leaderboard | Which model earns its cost? | model, calls, tokens, cost, ms, rating |
| Spend over time | Where did the money go, and when? | time, cost, model |
| Effort and mode | Does higher effort buy better ratings, or just cost? | effort, mode, rating, cost |
| Session drill-down | What happened in this one session? | session, turn, model, tokens, cost, ms, rating |

Deliberately **not** in v1: A/B replay comparison, which needs experiment machinery and a way to
re-run a turn, neither of which exists; and any view of prompt content, which is not recorded.

### 4. Rendering

- Charts are `<svg>` generated in Go from the aggregate. No client-side library, no CDN, no build
  step.
- One embedded `index.html` shell plus inline CSS, served from `internal/dash`, which already exists
  as a sentinel.
- Every number on a page is one a `kolk stats` invocation could also print, so the dashboard is a
  nicer view of the same truth rather than a second source of it.
- Empty state is a first-class screen: a new user opening `kolk dash` sees what will appear here and
  how to get it, not an axis with no line.

### 5. Budgets

No dependency change. Expected binary growth is the HTML shell and the SVG code, well inside the
12 MB soft budget from today's 7.91 MB. The closing leaf re-measures binary size and cold start, as
every budget-touching checkpoint does.

## Rationale

- **The measurement is the argument.** 578 ms for a year of heavy use, on the existing code path, on
  a laptop. SQLite would make it faster than imperceptible while costing a hard budget gate.
- **The dependency ceiling is a real gate**, not advice: `scripts/check-budgets.sh` fails above two
  modules. Spending that on a chart is not the trade this project wants; item 12 made the same call
  for sessions.
- **Server-side SVG removes a whole category of problem**: no asset pipeline, no vendored JavaScript
  to keep current, no scripting requirement, and it renders identically in a screenshot or a text
  browser.
- **One reader, one truth.** `kolk stats` and `kolk dash` reading the same file through the same
  functions means a number can be wrong, but the two surfaces cannot disagree about it.

## Alternatives rejected

- **SQLite via `modernc.org/sqlite`** — third module, fails the budget gate, and buys speed nobody
  can perceive at the measured volume. Revisit at the 2 s threshold, with a fresh measurement.
- **uPlot or any JS chart library** — 22 KB is cheap, but it adds a vendored third-party asset, an
  update obligation, and a scripting requirement for a page of four static charts.
- **A separate dashboard database written from `stats.jsonl`** — two stores that can disagree, to
  avoid parsing 28 MiB in half a second.
- **Storing prompt hashes** — Kolkrabbi does not store prompts, so there is nothing to hash.
- **Binding a friendly fixed port** — a predictable port on a machine with several users is a
  collision and a slightly wider door; port 0 and printing the URL is better on both counts.

## Risks & open questions

- **JSONL grows without bound.** 28 MiB a year for heavy use is fine; 10 years is not obviously fine
  → mitigation: `kolk stats --since` and a documented `stats.jsonl` rotation before the 2 s
  threshold, not after.
- **A corrupt line breaks a whole load** → `stats.Load`'s behaviour on malformed lines must be
  checked and, if it fails the load, changed to skip and count them: one bad append should not cost
  a user their history.
- **Ratings are sparse.** A leaderboard sorted by rating with three rated turns is noise wearing a
  ranking's clothes → mitigation: show the rated-turn count beside every rating and refuse to rank
  on fewer than a stated minimum.
- **Open:** whether `kolk dash` should offer CSV export. It is cheap, but it is also the first step
  toward data leaving the machine, which the product promises it does not do on its own.

## Sources

- Measurement on the development host, 2026-08-26: 105,000 synthetic records (28 MiB) aggregated by
  the shipped `kolk stats` in 578 ms wall clock.
- `scripts/check-budgets.sh` — the two-module ceiling and the 12/20 MB binary budgets, read in the
  tree 2026-08-26.
- `docs/research/dashboard.md` — the SQLite and uPlot proposal this supersedes, and the OTel GenAI
  column naming that `stats.Record` already follows.
- `internal/stats/stats.go` — `Load`, `Aggregate` and `Render`, which the dashboard reuses.
