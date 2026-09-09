# Kolkrabbi — optimization plan

Written 2026-09-08 from a review of the tree on `main` (v1.3.2). This file is a queue of
engineering leaves in the style of [`FABLE_OPTIMIZATION.md`](FABLE_OPTIMIZATION.md): one program,
small steps, one at a time, each ticked only when it is done **and** its gates are green. It is
deliberately not `docs/plan/NN-*.md`, because `make plan-check` requires every numbered doc to have
a PLAN.md item, and this is engineering, not a product decision.

Status: `[ ]` queued · `[~]` in progress · `[x]` done · `[!]` blocked

---

## Why this program exists

Kolkrabbi's headline promise is *fast and lightweight*. The build, the cold start, and the sandbox
overhead all sit inside their budgets. What is **not** inside any budget is the cost of a turn once
the model starts talking: every streamed token pays a synchronous `fsync`, every message rewrites
and double-syncs the whole transcript, and every vendor-CLI turn resends the whole conversation.
None of that shows in `make budgets`, because the budgets measure `kolk help`, not a session.

The second half of the program is maintainability: the two packages that hold half the code
(`internal/engine`, `internal/cli`) are flat, four functions are several hundred lines long, and the
test suite runs three times per `make check` while never running in parallel and never under
`-race` in CI.

## The rules that bind everything

Inherited from `AGENTS.md`, `docs/plan/02-architecture.md` and `FABLE_OPTIMIZATION.md`:

1. **Stdlib only; the engine touches no OS.** `make purity`, `make arch`, and the two-dependency
   budget stay green. Nothing here adds a module.
2. **One leaf, one owner, one dossier.** Red test → green → adversarial mutation → `make check` →
   entry in `docs/build-log.md`. Never two leaves in flight.
3. **Measure before and after.** Every performance leaf starts with a benchmark that is kept in
   the suite. A leaf whose benchmark does not move is reverted, not merged.
4. **Durability is a product promise.** `README.md` says sessions "auto-save after every step
   (atomic writes)" and a transcript "is the one thing here a person cannot reconstruct". No leaf
   may trade a committed transcript for speed. The event spill file is *not* that promise; it is a
   replay window for `kolk serve` clients.
5. **Zero-config stays zero-config.** No new required setting. Tunables ship with a computed
   default and are discoverable later.

## Verified ground truth (this review, 2026-09-08)

Read directly from the tree. Re-verify before building on any row; do not re-litigate.

| Fact | Where |
|---|---|
| The event bus is created with a spill file for **every** interactive session, not only `kolk serve` | `internal/cli/run.go:347` |
| `Bus.Publish` scrubs, encodes, writes the frame, then `Sync()`s the spill file, all under `b.mu` | `internal/bus/bus.go:196-257` |
| One `Publish` per streamed content token (`EventMessageDelta`) | `internal/engine/agent.go:1516-1523` |
| The spill file is opened `O_APPEND` and replayed in full on open; nothing truncates or rotates it | `internal/bus/bus.go:152-188` |
| Streamed tool-call arguments are accumulated with string `+=`; content uses `strings.Builder` | `internal/provider/client.go:489`, `:518` |
| `Session.Save` marshals the whole session with `MarshalIndent` and writes through `atomicfile.Write` (temp file, `Sync`, rename, directory `Sync`) | `internal/session/session.go:228-247`, `internal/atomicfile/atomicfile.go:47-96` |
| `Agent.save()` is called at least three times per turn: after the turn, and inside the tool loop per round | `internal/engine/agent.go:1488`, `:1577`, `:1634` (16 call sites in the engine) |
| Vendor CLI backends send the whole conversation every turn **and** `--resume` the vendor conversation | `internal/provider/agentcli/backend.go:136-137`, `:238-239` |
| Startup folds the entire `stats.jsonl` for model ratings (twice: `run.go` and `candidates.go`) | `internal/cli/run.go:830`, `internal/cli/candidates.go:29`, `internal/stats/ratings.go:19` |
| `kolk -r` loads and unmarshals **every** session file to pick the newest for this directory | `internal/cli/run.go:806` → `session.LatestForDir` → `session.List` → `session.Load` |
| Every start runs vendor discovery in the background for every enabled connector (spawns `codex --version`, `codex debug models`, …) and rewrites `vendor-models.json` whenever any vendor answered | `internal/cli/run.go:226`, `internal/cli/model_discovery.go:173-248` |
| `make check` runs the test suite **three** times: `scripts/test.sh` runs each module once for pass/fail and once `-v` for the count; `scripts/check-budgets.sh` runs the root module `-v` again | `scripts/test.sh:17-20`, `scripts/check-budgets.sh:73` |
| 37 `time.Sleep` calls in 23 test files; **zero** `t.Parallel()` anywhere | `rg time.Sleep --glob '*_test.go'` |
| CI runs no `-race` and collects no coverage; golangci-lint is `version: latest` while every action is SHA-pinned | `.github/workflows/ci.yml:125-127` |
| Binary soft budget (12 MB) is a warning; README claims "~5MB static binary, ~2ms startup" | `scripts/check-budgets.sh:10,30`, `README.md:18` |
| `cmd/kolkd` and `cmd/kolk-mock` have no tests | `cmd/**/*_test.go` → none |
| Largest files: `engine/agent.go` 1,654 · `tui/model.go` 1,226 · `tui/controller.go` 964 · `tui/runtime.go` 961 · `cli/run.go` 944 · `protocol/events.go` 936 · `engine/orchestrator.go` 803 · `cli/tui_repl.go` 798 | `wc -l` |
| Longest functions: `newAgent` (`cli/run.go:164`), `runConfig` (`cli/cmd_config.go:16`), `slash` (`cli/slash.go:183`), `validateEventData` (`protocol/events.go:444`) | **measured in O0 on 2026-09-09: 329 / 553 / 409 / 461 lines** (`func` line to its closing brace at `HEAD`); the 09-08 pass reported 328 / 530 / 408 / 460 |
| `go build ./...` clean; two direct deps (`x/sys`, `x/term`); Go 1.25 | `go.mod` |

**Reported but not re-verified here** (from the 2026-09-08 review pass whose artefacts were lost;
treat as hypotheses until O0 re-measures them): release binary 9.4 MB, cold start p50 6.6 ms, full
suite 57.5 s wall / 3,533 tests with `cli` + `shell` at 63 % of the time, spill `fsync` ≈ 2.8 ms per
publish vs 3 µs without, tool-argument `+=` at 200 KB ≈ 82 ms and 1.05 GB allocated, `Save` at
4.8 MB ≈ 23 ms.

**O0 has now measured all but the first two** (2026-09-09, see the table under O0): the suite is
23.9 s wall / 3,569 `=== RUN` lines with `cli` + `shell` at 46 %; the spill `fsync` is 3.28 ms
against 49 µs in memory; tool-argument `+=` at 200 KB is ~265 ms and 2.43 GB; `Save` at 5 MB is
40 ms. Release binary size and cold start remain unmeasured hypotheses and belong to O12.

---

## Findings ledger

| ID | Sev | Where | Finding | Phase |
|---|---|---|---|---|
| O1 | **P0** | `bus/bus.go:232-237`, `engine/agent.go:1519` | `fsync` per streamed token, under the bus mutex, in every session; spill file grows without bound | 1 |
| O2 | P1 | `provider/client.go:518` | Quadratic string `+=` for streamed tool arguments | 1 |
| O3 | P1 | `session/session.go:228`, `engine/agent.go:1488/1577/1634` | Full-transcript `MarshalIndent` + file `fsync` + dir `fsync` on every save, several saves per turn | 2 |
| O4 | P1 | `provider/agentcli/backend.go:136` | Whole conversation resent every vendor-CLI turn while also resuming: O(N²) prompt bytes per session | 3 |
| O5 | P2 | `cli/run.go:830`, `cli/candidates.go:29`, `stats/stats.go:126` | Full `stats.jsonl` parse at startup, twice | 2 |
| O6 | P2 | `session/session.go:395`, `cli/run.go:806` | `kolk -r` unmarshals every session file to find one | 2 |
| O7 | P2 | `bus/bus.go:152-180` | Whole spill file replayed and re-encoded on every session resume | 2 |
| O8 | P2 | `cli/model_discovery.go:240`, `:229` | Vendor CLIs spawned on every start; catalog file rewritten even when unchanged | 2 |
| O9 | P2 | `scripts/test.sh:17-20`, `scripts/check-budgets.sh:73` | Suite runs three times per `make check` | 1 |
| O10 | P2 | 23 test files | 37 `time.Sleep`, 0 `t.Parallel`; no `-race` and no coverage in CI | 2 |
| O11 | P2 | `.github/workflows/ci.yml:127` | golangci-lint unpinned (`latest`) | 1 |
| O12 | P2 | `check-budgets.sh:30`, `README.md:18` | Size budget only warns; README size/startup claim unmeasured | 1 |
| O13 | P2 | `cli/run.go:164`, `cli/cmd_config.go:16`, `cli/slash.go:183`, `protocol/events.go:444` | Four functions of 300–530 lines; `newAgent` wires the whole engine inline | 3 |
| O14 | P3 | `internal/engine` (55 files), `internal/cli` (~90 files) | Flat packages mixing several concerns; `agent.go` 1,654 lines | 3 |
| O15 | P3 | `session/session.go:198-218` | `path()`, `CooldownsFile()`, `CkptDir()` panic on an invalid ID instead of returning an error | 1 |
| O16 | P3 | `bus/bus.go:197` | Regex scrub of every payload on the publish path, including per-token deltas | 1 (with O1) |
| O17 | P3 | `cmd/kolkd`, `cmd/kolk-mock` | No tests on two of three entry points | 2 |
| O18 | P3 | `tui/runtime.go` | Repeated cancel-and-dismiss blocks (reported ×5) | 3 |

---

## O0 — Baseline: measure before touching anything  ·  **done 2026-09-09**

**Observable:** a `bench/` target and a set of `Benchmark*` functions that print the numbers the
rest of this file quotes, kept in the suite, run by `make bench`.

- [x] **O0.1** `internal/bus`: `BenchmarkPublish/{memory,spill}` — 1 KB delta payload, 10,000
      publishes, report ns/op, allocs/op, and bytes written.
- [x] **O0.2** `internal/provider`: `BenchmarkReadStream/{content,toolargs}` at 50 KB and 200 KB
      of fragmented SSE using the `enginetest` fragmenter.
- [x] **O0.3** `internal/session`: `BenchmarkSave/{100KB,1MB,5MB}` into a `t.TempDir()`.
- [x] **O0.4** `internal/stats`: `BenchmarkRatingsByModel/{1k,20k}` records.
- [x] **O0.5** `internal/session`: `BenchmarkLatestForDir/{10,200}` sessions of 200 KB each.
- [x] **O0.6** A `scripts/bench.sh` that runs the five with `-count=3 -benchmem`, writes
      `bench/baseline.txt` once, and diffs later runs against it with `benchstat` **if installed**
      (not a dependency). Record the baseline numbers in `docs/build-log.md`.
- [x] **O0.7** `go test ./... -count=1 -json | jq` (or `-v` + a tiny awk) to list the twenty
      slowest tests; record them in the build log as the O10 target list.
- [x] **O0.8** Measure the four long functions with `gocyclo`-free arithmetic (start line → closing
      brace) and record the real lengths beside O13.

**Exit:** baseline file committed; every later leaf quotes a before/after pair from it.

**Measured 2026-09-09** — `bench/baseline.txt`, `make bench`, dossier in `docs/build-log.md`.
Median of three on an Apple M3 (go1.26.4, darwin/arm64) under desktop load; `B/op` and
`allocs/op` repeat to better than 0.1 % and are the durable half, wall clock spread reached 3×
on the longest rows. Re-run `BENCH_BASELINE=1 scripts/bench.sh` on the machine a leaf is judged
on rather than comparing across sessions.

| benchmark | median | B/op | allocs/op |
|---|---|---|---|
| `Publish/memory` | 49.0 µs | 11.9 KB | 28 |
| `Publish/spill` | 3.28 ms | 11.7 KB | 28 (1,192 spill bytes/event) |
| `ReadStream/content/50KB` | 5.35 ms | 4.73 MB | 95,155 |
| `ReadStream/content/200KB` | 21.5 ms | 18.76 MB | 380,418 |
| `ReadStream/toolargs/50KB` | 20.4 ms | 163.75 MB | 96,844 |
| `ReadStream/toolargs/200KB` | 265 ms | 2,434.81 MB | 387,076 |
| `Save/100KB` | 7.53 ms | 0.35 MB | 24 |
| `Save/1MB` | 12.3 ms | 5.43 MB | 45 |
| `Save/5MB` | 40.2 ms | 28.14 MB | 42 |
| `LatestForDir/10` | 14.9 ms | 4.56 MB | 2,330 |
| `LatestForDir/200` | 204 ms | 91.25 MB | 46,228 |
| `RatingsByModel/1k` | 5.15 ms | 1.57 MB | 12,246 |
| `RatingsByModel/20k` | 102 ms | 34.24 MB | 244,092 |

What this corrects above: the spill `fsync` is 67× the in-memory publish, not the ~1,000×
reported — the `fsync` guess (2.8 ms) was right, the in-memory guess (3 µs) was not, because a
1 KB delta pays a scrub, an encode and two clones for 28 allocations before the file is touched.
Tool-argument `+=` allocates 2.43 GB at 200 KB, not 1.05 GB, against 18.8 MB for the content
path at the same size: **130×**, and that ratio is O2's acceptance, not the wall clock. `Save`
at 5 MB is 40 ms, not 23 ms at 4.8 MB, so a 5 MB transcript costs ~120 ms a turn across three
saves. The suite is 3,569 `=== RUN` lines (2,200 top-level) across 38 packages, 23.9 s wall with
a warm cache and 90.1 s summed; `cli` + `shell` are 46 % of it, not 63 %. `time.Sleep` at 38
calls in 23 files and `t.Parallel()` at zero are confirmed.

---

## Phase 1 — Quick wins (each under a day, no design decision needed)

### O1 — Stop `fsync`ing every token  ·  P0

**Problem.** `Bus.Publish` writes one NDJSON frame and calls `Sync()` per event, with `b.mu` held
(`bus.go:232-237`). `RunTurn` publishes one event per streamed token (`agent.go:1519`), and every
interactive session has a spill file (`run.go:347`). On a laptop SSD an `fsync` is single-digit
milliseconds, so a 2,000-token answer spends seconds in `fsync`, serialised against subscribers.
The file is also `O_APPEND`-only with no rotation, so it grows for the life of the session.

**What the spill is for.** Replay of retained events to a late `kolk serve` client (Last-Event-ID
semantics) and recovery of the sequence counter across restarts. Neither needs per-token
durability; losing the last few deltas after a power cut costs nothing a person can notice, because
the transcript itself is in the session file (O3 owns that promise).

**Steps.**
1. Add `Options.SyncPolicy` with three values: `SyncNever`, `SyncOnTurnBoundary` (default),
   `SyncEvery`. Publish stops calling `Sync()` unless the policy says so.
2. On `SyncOnTurnBoundary`, `Sync()` when `event.Type` is `turn.finished`, `turn.cancelled`, or
   any `permission.*` event (those are the ones a client must not see twice), and from `Close()`.
3. Move the `Write` outside the critical section: encode and sequence under `b.mu`, hand the frame
   to a single writer goroutine through a bounded channel, and make `Close()` drain it. Fan-out to
   subscribers stays synchronous as today so ordering guarantees do not change.
4. Bound the spill file: when it exceeds `Options.MaxSpillBytes` (default 64 MB), rewrite it from
   the in-memory retained window (which `trim()` already bounds) into a temp file and rename. The
   replay contract is "retained window", so nothing observable is lost.
5. (O16) Skip `redact.ScrubJSON` for `message.delta` payloads whose text is under 64 bytes and
   contains no `=`/`:` characters — or, better, scrub once per completed message and let deltas
   through, since `message.completed` carries the same text and is scrubbed. Decide with a test
   that a secret split across two deltas is still scrubbed in `message.completed`.

**Files.** `internal/bus/bus.go`, `internal/bus/bus_test.go`, `internal/bus/spill_test.go`,
`internal/cli/run.go:347`, `internal/cli/cmd_serve.go:54`.

**Success.** `BenchmarkPublish/spill` within 3× of `BenchmarkPublish/memory` (today it is
reported at ~1,000×); a 5,000-delta turn spends < 10 ms total in the bus; existing spill replay
tests unchanged; new test proves a spill file over the cap is rewritten and still replays the
retained window.

**Risk / rollback.** A client reconnecting after a crash may miss the final deltas of the
interrupted turn; it already re-reads `message.completed` from the session, so document it in
`docs/plan/` and `protocol/`. Rollback is `SyncPolicy: SyncEvery` in `run.go`, one line.

- [ ] O1.1 benchmark first (O0.1) · [ ] O1.2 policy + turn-boundary sync · [ ] O1.3 writer
      goroutine · [ ] O1.4 spill cap · [ ] O1.5 delta scrub decision · [ ] O1.6 `make check` +
      dossier

### O2 — `strings.Builder` for streamed tool arguments  ·  P1

**Problem.** `existing.Function.Arguments += tc.Function.Arguments` (`client.go:518`) copies the
whole accumulated string per fragment. Content deltas already use a builder (`:489`). A 200 KB
`write_file` argument arrives in hundreds of fragments and allocates on the order of a gigabyte.

**Steps.**
1. Keep a `map[int]*strings.Builder` beside `toolCalls` for the argument text (and one for the
   name, which is also `+=`'d at `:516`); materialise into `ToolCall.Function.Arguments` once at
   the end where `msg.ToolCalls` is assembled.
2. Add the fragmented fixture at 200 KB to `stream_fuzz_test.go`'s seed corpus.

**Files.** `internal/provider/client.go`, `internal/provider/client_test.go`.

**Success.** `BenchmarkReadStream/toolargs` allocations drop by > 90 %; time linear in size.

**Risk / rollback.** None observable; pure refactor. Revert the commit.

- [ ] O2.1 benchmark (O0.2) · [ ] O2.2 builder · [ ] O2.3 corpus · [ ] O2.4 gates

### O9 — Run the test suite once per `make check`  ·  P2

**Problem.** `scripts/test.sh` runs each module twice (pass/fail, then `-v` for the count), and
`scripts/check-budgets.sh:73` runs the root module a third time for the test-count floor and the
sandbox-overhead line. That triples the longest step of CI.

**Steps.**
1. `test.sh`: run once with `-v -count=1`, tee to a log, count `=== RUN` from the log, propagate the
   exit status through `${PIPESTATUS[0]}`.
2. `test.sh` writes the log to `${KOLK_TEST_LOG:-$(mktemp)}`; `check-budgets.sh` reuses it when the
   variable is set and the file is newer than `go.sum`, otherwise runs the suite itself (so
   `make budgets` alone still works).
3. Wire `KOLK_TEST_LOG` in the `Makefile` `check` target and in `ci.yml`'s `budgets` job (which
   runs on a separate runner, so it will still test once — that is fine, it is one run, not two).
4. Raise `TEST_FLOOR` from 22 to a number that can trip: 90 % of the current count, and bump it
   with each release (O12 owns the same ratchet for size).

**Files.** `scripts/test.sh`, `scripts/check-budgets.sh`, `Makefile`, `.github/workflows/ci.yml`.

**Success.** `time make check` drops by roughly two suite runs; test count still printed and
enforced; a deliberately failing test in a nested module still fails `make test`.

**Risk / rollback.** `-v` output for a passing run is large; keep it in the temp file only. Revert
the three scripts.

- [ ] O9.1 single run + PIPESTATUS · [ ] O9.2 shared log · [ ] O9.3 Makefile/CI wiring ·
      [ ] O9.4 floor ratchet

### O11 — Pin golangci-lint  ·  P2

Replace `version: latest` with the exact version that passes today (check `golangci-lint version`
locally), and add it to `make workflow-pin-check`'s contract so an unpinned tool version fails CI
the same way an unpinned action does. Also print the pinned version in the `make lint` install hint.

**Files.** `.github/workflows/ci.yml:127`, `scripts/check-workflow-pins.sh` (or whichever script
`workflow-pin-check` runs), `Makefile:91`.

- [ ] O11.1 pin · [ ] O11.2 guard test

### O12 — Make the size budget a budget, and the README true  ·  P2

1. Turn the 12 MB soft limit into a **ratchet**: `BIN_HARD` becomes `max(current release size +
   10 %, …)` recorded in `scripts/check-budgets.sh` and bumped only in a commit that says why.
   Keep 20 MB as the absolute ceiling.
2. Measure the actual stripped build (`make build`) and cold start and put the real numbers in
   `README.md:18` and `site/` (`make site` checks the landing page; extend it to assert the README
   and site quote the same figure).
3. Add `go tool nm -size -sort size` top-30 to `docs/build-log.md` once so the next person knows
   where the bytes are; if `net/http` + TLS dominate, that is the accepted cost and the note says so.

**Files.** `scripts/check-budgets.sh`, `README.md`, `site/`, `scripts/test-site.sh`.

- [ ] O12.1 ratchet · [ ] O12.2 measured claims · [ ] O12.3 size map

### O15 — Errors, not panics, for a bad session ID  ·  P3

`path()`, `CooldownsFile()`, `CkptDir()` panic on an invalid ID. `Load` validates before
constructing, so the panic is unreachable from disk today, but any future constructor that forgets
`validateSessionID` turns a corrupt file into a crash. Change the three to return `(string, error)`
or validate once in a private constructor and make `Session.ID` unexported-settable. Pick the one
with the smallest diff (probably: validate in every constructor, keep the panics as
`panic("session: unvalidated ID; construct through New/Load")` with a test that every constructor
validates).

**Files.** `internal/session/session.go`, callers in `internal/cli/cmd_sessions.go`.

- [ ] O15.1

**Phase 1 exit.** O1, O2, O9, O11, O12, O15 ticked; `bench/baseline.txt` shows O1 and O2 moved;
`make check` green and measurably shorter.

---

## Phase 2 — Medium (one to three days each, a small design decision each)

### O3 — Save the transcript once per boundary, not once per message  ·  P1

**Problem.** `Session.Save` marshals the whole session with indentation and does temp-write,
`fsync`, rename, directory `fsync` (`atomicfile.go:47-96`). The engine saves after every model
round and every tool round (`agent.go:1488`, `:1577`, `:1634`, plus 13 other sites). A 50-round
agent turn on a multi-megabyte session performs ~100 full rewrites.

**Design decision (recommendation first).**
- **A. Coalesce, keep the format.** `Agent.save()` marks dirty; a `saveNow()` runs at turn end,
  before any tool that mutates files, before every permission prompt, on `/compact`, `/undo`,
  `/new`, on pause, and on exit. Inside the tool loop, save at most once per `Options.SaveInterval`
  (default 2 s) unless the round wrote a file. Format unchanged, resume unchanged. **Recommended**:
  smallest diff, keeps the README promise (every *step* that matters is still durable).
- **B. Append-only journal + periodic snapshot.** Messages appended to `<id>.journal.ndjson` with
  one `fsync` per append; the JSON snapshot rewritten at turn boundaries; `Load` replays journal
  over snapshot. Faster still, but a second on-disk format, a migration, and every session tool
  (`sessions export`, `fork`, `search`, `doctor`) learns the journal. Defer unless A is not enough.
- **C. Drop `MarshalIndent` for `Marshal`.** Cheap, cuts bytes ~30 %, but `sessions export` and
  humans read these files; keep indentation, or indent only in `export`.

**Steps (A).**
1. `BenchmarkSave` (O0.3) first.
2. Introduce `Agent.markDirty()` and `Agent.flush(reason)`; replace the 16 `a.save()` call sites
   with one or the other, table-driven: which reasons flush immediately, which mark.
3. `atomicfile.Write` gains `WriteOptions{SkipDirSync bool}`; the interval save uses it and the
   boundary save does not (a rename's durability only matters at a boundary).
4. Test: a session that is killed (SIGKILL, via `TestMain` re-exec) mid-tool-loop resumes with
   the last flushed boundary and the interrupted tool call repaired — the existing repair code at
   `agent.go:780-808` is the safety net; prove it holds with a flush interval of 1 h.
5. `-race` on `engine` and `session`; `save_race_test.go` extended for the dirty flag.

**Files.** `internal/engine/agent.go`, `internal/engine/{chain,pause,undo,resume,compact,orchestrator}.go`,
`internal/session/session.go`, `internal/atomicfile/atomicfile.go`.

**Success.** Saves per 50-round turn ≤ 50 → target ≤ 10 with file writes; `BenchmarkSave` itself
unchanged (the win is call count, not per-call cost); README's "auto-save after every step" still
true for steps that change files or need a prompt.

**Risk / rollback.** A crash between two interval saves loses up to one interval of chat-only
messages. Bound it at 2 s and flush before every user-visible prompt. Rollback: set the interval
to 0, which restores save-on-every-call.

- [ ] O3.1 bench · [ ] O3.2 dirty/flush table · [ ] O3.3 dir-sync option · [ ] O3.4 kill test ·
      [ ] O3.5 gates + dossier

### O5 — Ratings without reading the whole log  ·  P2

**Problem.** `RatingsByModel` loads every record in `stats.jsonl` (`stats.go:126`) and folds it via
`Aggregate`; startup calls it from `run.go:830` and again from `candidates.go:29`. The file grows
by one line per model call forever.

**Steps.**
1. Call it once: compute in `newAgent`, pass the map to the candidate ranking.
2. Cache the fold: write `ratings.json` beside `stats.jsonl` with `{offset, modtime, ratings}`;
   on load, if the log is unchanged reuse, else fold only the bytes after `offset` (ratings are
   an associative fold per model, so partial re-fold is exact). Invalidate on `/rate` writes.
3. Keep `kolk stats` reading the whole log; it is a report, not a startup path.

**Files.** `internal/stats/ratings.go`, `internal/stats/stats.go`, `internal/cli/run.go`,
`internal/cli/candidates.go`.

**Success.** `BenchmarkRatingsByModel/20k` warm path < 1 ms; startup with a 20k-record log within
1 ms of an empty log.

**Risk / rollback.** A stale cache after a hand-edited log; the `modtime+size` check covers
edits; `kolk doctor` gets a "ratings cache rebuilt" line. Rollback: delete `ratings.json`.

- [ ] O5.1 · [ ] O5.2 · [ ] O5.3

### O6 — `kolk -r` without unmarshalling every session  ·  P2

**Problem.** `LatestForDir` → `List` → `Load` reads and decodes every `<id>.json` (`session.go:395`).
With a few hundred sessions of a few hundred KB each that is tens of MB of JSON per resume, and
`kolk sessions`, `doctor`, and the serve session picker do the same.

**Steps.**
1. Order by `os.ReadDir` modtime first and stop at the first session whose `CWD` matches — but
   `CWD` needs the decode, so:
2. Write a small header file per session, `<id>.meta.json` (`ID, Title, CWD, UpdatedAt, Model,
   MessageCount`), updated inside `Save` (it is a few hundred bytes; no `fsync` needed, the session
   file is the source of truth). `List` reads metas, falls back to the full decode for a session
   with no meta (migration path), and `Load` is only called for the one chosen.
3. `sessions list/search` use metas; `search` on message text still decodes, but only the
   candidates whose title did not match.

**Files.** `internal/session/session.go`, `internal/session/overview.go`, `internal/cli/cmd_sessions.go`,
`internal/cli/serve_session_pick.go`, `internal/cli/cmd_doctor.go`.

**Success.** `BenchmarkLatestForDir/200` under 5 ms; `kolk -r` resume time flat in session count.

**Risk / rollback.** Meta drift from the session file: `Save` writes both, `doctor` verifies and
repairs. Rollback: ignore metas (`List` still works from full decode).

- [ ] O6.1 · [ ] O6.2 · [ ] O6.3

### O7 — Bounded spill replay on resume  ·  P2

Once O1.4 caps the spill file this mostly solves itself. Remaining step: on open, seek to the
last `MaxBytes` of the file (or the last N frames) instead of decoding from the start, and set
`latest`/`lastTime` from the final frame. Requires a frame-aligned backward scan: read the tail
chunk, drop the partial first line, decode the rest.

**Files.** `internal/bus/bus.go:152-180`, `internal/bus/spill_test.go`.

- [ ] O7.1

### O8 — Discover vendors when it matters, not on every start  ·  P2

**Problem.** `refreshVendorCatalogsInBackground` runs on every `newAgent` (`run.go:226`) and spawns
each enabled vendor CLI (`codex --version`, `codex debug models`, …) with a 15 s bound each; it
then rewrites `vendor-models.json` whenever any vendor answered (`model_discovery.go:229`), even
if nothing changed. Background or not, it competes with the first turn for CPU and disk, and it
churns the file's mtime.

**Steps.**
1. Stale-while-revalidate with a TTL: skip a vendor whose catalog was fetched less than
   `vendorCatalogTTL` ago (default 6 h; the gateway catalog already has this shape) unless
   `--refresh`, a login, or a `VendorVersion` mismatch (cheap: `codex --version` only) says
   otherwise.
2. Compare the new catalog to the stored one and skip `SaveVendorCatalogs` when equal.
3. Start the background discovery **after** the first prompt is shown and yield to the turn: run
   it on a goroutine that checks `ctx` and pauses while a turn is streaming (a single atomic flag
   the agent sets).

**Files.** `internal/cli/model_discovery.go`, `internal/provider/vendor_catalog_store.go`,
`internal/cli/run.go:226`.

**Success.** A warm start spawns zero vendor processes; `TestStartupDiscoversEveryEnabledConnectorInTheBackground`
becomes "…OnceUntilStale"; `vendor-models.json` mtime unchanged across warm starts.

**Risk / rollback.** A vendor that renames a model inside the TTL is seen up to 6 h late; the
first turn's `unrecognized_model` path (F4.3) already marks it `gone` on contact. Rollback: TTL 0.

- [ ] O8.1 TTL · [ ] O8.2 no-op save · [ ] O8.3 yield to the turn

### O10 — A faster, parallel, race-checked suite  ·  P2

**Problem.** 37 `time.Sleep` in tests, none of them behind a clock abstraction; no `t.Parallel`;
CI never runs `-race`; no coverage number exists.

**Steps.**
1. From O0.7's slowest-twenty list, replace each `time.Sleep` with a channel, `sync.WaitGroup`,
   `context` cancellation, or the existing `FakeClock` (reported unused — this is its job). Where
   a sleep is a *timeout*, keep it but make it the failure path only.
2. Add `t.Parallel()` package by package, starting with the pure ones (`protocol`, `provider`,
   `stats`, `session`, `bus`, `diff`, `redact`); anything touching `os.Chdir`, `t.Setenv`, or a
   fixed port stays serial and says why in a comment.
3. CI: add `go test -race ./internal/engine/... ./internal/cli/... ./internal/provider/...
   ./internal/shell/... ./internal/bus/...` as its own job on Ubuntu (macOS `-race` is slow; skip).
4. CI: `-coverprofile` on the Ubuntu test job, upload as an artifact, print the total. No third-party
   coverage service, no badge, no threshold in the first leaf; add a ratchet floor in a second leaf
   once the number is known.
5. `cmd/kolkd` and `cmd/kolk-mock` (O17): one smoke test each (`--help` exits 0; mock serves a
   scripted turn on a loopback port).

**Files.** the 23 test files, `.github/workflows/ci.yml`, `cmd/kolkd/main_test.go`,
`cmd/kolk-mock/main_test.go`.

**Success.** Wall time of `scripts/test.sh` halved from the O0.7 baseline; `-race` green on every
push; coverage number in the build log.

**Risk / rollback.** Parallel tests expose real races — that is the point; each one found is a
finding for the ledger, not a reason to serialise. Rollback per package by removing `t.Parallel()`.

- [ ] O10.1 sleeps · [ ] O10.2 parallel · [ ] O10.3 race job · [ ] O10.4 coverage · [ ] O10.5 cmd tests

**Phase 2 exit.** O3, O5, O6, O7, O8, O10 ticked; a 200-session, 20k-record profile starts and
resumes as fast as an empty one; CI runs the suite once, in parallel, under `-race`.

---

## Phase 3 — Long-term (design first; each is a `docs/plan` conversation or an owner decision)

### O4 — Send the delta, not the transcript, to a resumed vendor conversation  ·  P1

**Problem.** `promptFromMessages` sends the whole conversation every turn (`backend.go:136`) even
when the process was opened with `--resume` on a handle the vendor already holds (`:238`). Over a
session that is O(N²) bytes through the child's stdin and into the vendor's context, and for a
metered gateway it is O(N²) *tokens*. The comment explains why: a killed process leaves the
vendor's turn unfinished, and resending is how kolk's transcript stays authoritative.

**Design (owner decision — the trade is cost vs. certainty).**
- **A. Delta on a confirmed resume.** Track `vendorSawUpTo int` (message index) on the backend; on
  a turn where `session.Resumed()` and the previous turn ended cleanly (`stop_reason` observed, no
  `HardExit`), send only messages after `vendorSawUpTo`. On any doubt (hard exit, unusable session,
  fresh handle, kolk restart), fall back to the full replay as today. **Recommended.** Keeps the
  existing safety story; the full replay remains the rule for every abnormal path.
- **B. Always full replay, but compacted.** Reuse `/compact`'s summariser on the vendor prompt when
  it exceeds a byte threshold. Cheaper to build, still O(N) per turn, and the vendor sees a summary
  it did not write.
- **C. Trust the vendor's memory entirely.** Send only the new user message when resumed. Simplest,
  but the comment at `backend.go:130-137` is the record of why that was rejected once.

**Steps (A).** Benchmark bytes-per-turn over a 30-turn scripted session in `backend_test.go`;
implement the watermark; adversarial tests for kill-mid-turn, handle expiry, kolk restart, `/undo`
(which rewinds kolk's transcript below the watermark and must force a full replay); document in
`docs/plan/04-subscription-backends.md`.

**Files.** `internal/provider/agentcli/backend.go`, `session.go`, `translate.go`, `backend_test.go`.

**Success.** Prompt bytes per turn flat in session length on the happy path; every abnormal path
test still sends the full transcript.

- [ ] O4.0 owner decision · [ ] O4.1 bench · [ ] O4.2 watermark · [ ] O4.3 adversarial · [ ] O4.4 docs

### O13 — Four functions become tables and small functions  ·  P2

Order by leverage:
1. **`newAgent` (`cli/run.go:164`)** blocks every other change to startup (O5, O6, O8 all edit it).
   Split into `resolveEndpointAndKey`, `pickModel`, `openSession`, `openBus`, `buildToolset`,
   `wireSubagents`, `startBackground`, each returning a value that the next consumes; `newAgent`
   becomes ~40 lines of sequence. No behaviour change; the existing `first_run_test.go`,
   `default_model_test.go`, `plan_backend_test.go` are the net.
2. **`validateEventData` (`protocol/events.go:444`)** is a switch over event types; make it a
   `map[EventType]func(json.RawMessage) error` populated in `init`, one validator per event in the
   file that defines the event's data type. `event_catalog_test.go` already enumerates every type,
   so it can assert every type has a validator.
3. **`slash` (`cli/slash.go:183`)**: a `map[string]slashCommand{Run, Help, Aliases}` — the TUI's
   `commands.go` already has a command table; there should be one, shared.
4. **`runConfig` (`cli/cmd_config.go:16`)**: subcommand table, one function per `config` verb.

One function per leaf; each leaf is a pure refactor with `make check` green and no test edited
except to add one. Measure with the O0.8 arithmetic before and after.

- [ ] O13.1 newAgent · [ ] O13.2 validateEventData · [ ] O13.3 slash · [ ] O13.4 runConfig

### O14 — Package boundaries inside `engine` and `cli`  ·  P3

Not a big-bang split. Extract along seams that already exist in file names, one sub-package per
leaf, with `make arch` updated to name each new package's layer:
- `internal/engine/saga` (the `saga_*.go` files, ~2,300 lines) — self-contained already.
- `internal/engine/subagent` (`subagent_*.go`, `orchestrator.go`, `roster.go`, `level_routing.go`).
- `internal/cli/cmd` for the `cmd_*.go` files, leaving `run.go`, `repl.go`, `tui_repl.go`, `flags.go`
  in `cli`.
Stop when `agent.go` is under 800 lines and no package has more than 30 non-test files. Do this
**after** O13.1, which is what makes the `cli` split possible.

- [ ] O14.1 saga · [ ] O14.2 subagent · [ ] O14.3 cli/cmd

### O18 — TUI duplicate blocks  ·  P3

Extract the repeated cancel-and-dismiss sequence in `tui/runtime.go` into one method; confirm the
count with `rg` first (reported ×5). Also fold the reported duplicate helpers (`firstLine`,
`humanBytes` with differing output, hand-rolled `contains`/`min64` on Go 1.25) into one each using
`strings.Contains`, `min`, and a shared `internal/text` helper if one is warranted.

- [ ] O18.1

---

## How success is measured

| Metric | Where measured | Baseline (O0) | Target |
|---|---|---|---|
| Bus publish, spill vs memory | `BenchmarkPublish` | reported ~1,000× | ≤ 3× |
| Tool-args stream, 200 KB | `BenchmarkReadStream/toolargs` | reported 82 ms / 1.05 GB | < 5 ms / < 5 MB |
| Session saves per 50-round turn | counter in `engine` test | ~100 | ≤ 10 |
| Startup with 20k stats records | `BenchmarkRatingsByModel` + `check-budgets` cold start | reported 57 ms | < 1 ms warm |
| `kolk -r` with 200 sessions | `BenchmarkLatestForDir` | (measure) | < 5 ms |
| Warm-start vendor processes | test counter | every start | 0 inside TTL |
| Vendor prompt bytes, turn 30 vs turn 1 | `backend_test` | O(N) | flat on happy path |
| `make check` suite runs | scripts | 3 | 1 |
| `scripts/test.sh` wall time | CI log | reported 57.5 s | ≤ 30 s |
| `-race` in CI | `ci.yml` | none | every push |
| Coverage | CI artifact | unknown | measured, then ratcheted |
| Longest function | O0.8 arithmetic | reported 530 lines | < 120 lines |
| Binary size claim vs measured | `check-budgets`, README | "~5 MB" vs reported 9.4 MB | one number, ratcheted |

Every performance leaf records before/after from `scripts/bench.sh` in `docs/build-log.md`; a
leaf whose target is not met is either reverted or its target is re-argued in this file, in
writing, before it merges.

## Order and stop rules

`O0 → O1 → O2 → O9 → O11 → O12 → O15 → O3 → O5 → O6 → O7 → O8 → O10 → O4 → O13 → O14 → O18`.

- O0 first; nothing else merges without its baseline.
- O1 outranks everything: it is the only P0 and it is on every session's hot path.
- O3 needs a decision (A recommended) but not an owner; O4 needs the owner (cost vs. certainty).
- O13.1 (`newAgent`) may be pulled forward if O5/O6/O8 start fighting over `run.go`.
- Any new P0/P1 found on the way goes into the ledger and the earliest phase that owns its file.
- Stop a leaf and record `[!]` when its benchmark does not move or a gate goes red for a reason the
  leaf did not cause.

## Non-goals

- No new dependency (no SQLite, no benchstat requirement, no coverage SaaS).
- No change to the on-disk session JSON format in this program (O3 option B is deferred; O6's meta
  file is additive and disposable).
- No change to the protocol's event vocabulary or ordering guarantees; O1 changes durability
  timing only, and says so in `protocol/`.
- No prompt-level change to any vendor backend beyond the O4 watermark.
- No package split before O13.1 lands.

## Sources

- File reads and `rg`/`wc` over the tree at v1.3.2, 2026-09-08 (every row in "Verified ground
  truth" names its line).
- `FABLE_OPTIMIZATION.md` F5 (per-turn efficiency, done) and its non-goal "no performance work
  beyond what F5.1 measures" — this file is that work, measured.
- `docs/plan/02-architecture.md` §11 budgets; `scripts/check-budgets.sh`.
- The 2026-09-08 review pass (structure / performance / quality documents, artefacts lost to a
  session limit); its numbers are quoted as "reported" and O0 re-measures them.
