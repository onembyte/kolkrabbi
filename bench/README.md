# KolkBench

A benchmark for **coding-agent harnesses**, not for models.

Three terminal agents get the same model, the same repository and the same prompt. What differs is
the agent loop around the model — how it explores, when it stops, how it recovers from a bad edit.
That is the number nobody publishes, and the one a harness can actually be judged on.

- **[methodology.md](methodology.md)** — what is measured, how the model is held constant, the
  controls against the obvious bias, what this does *not* measure, and how to attack it.
- **[harnesses.md](harnesses.md)** — the exact command line and configuration for every harness.
- **`tasks/`** — one directory per task: the prompt, a small fixture, and a machine-checked
  `verify.sh`.
- **`results/`** — raw per-run JSONL and full transcripts. Published as produced.

## Status

**No comparative run has happened.** Tasks are pre-registered and the runner is proven against a
scripted mock; `results/` holds nothing yet. Any summary of this benchmark that exists before that
directory does is not describing this benchmark.

## Running it

    # one task, one harness
    bench/run.sh --harness kolk --task 01-go-off-by-one --runs 5 \
      --base-url http://127.0.0.1:11434/v1 --model qwen2.5-coder:14b

    # everything
    bench/run.sh --harness kolk --task all --runs 5 \
      --base-url http://127.0.0.1:11434/v1 --model qwen2.5-coder:14b

Every run copies the fixture to a temporary directory and `git init`s it, so no run can see what a
previous one did. `verify.sh`'s exit code is the entire result; nothing is scored by reading output.

Fixtures need no network and no dependency installation — stdlib test runners only.

## The tasks

Twelve, pre-registered. `00-smoke` is plumbing and is excluded from published results.

| Task | Kind | What it is |
|---|---|---|
| `01-go-off-by-one` | fix | A loop that misses the last element. The easy one, on purpose |
| `02-go-write-test` | write-test | Write a test for an untested function. **Mutation-checked**: the function is then broken, and a test that still passes has tested nothing |
| `03-py-parse-bug` | fix | `split("=")` where the value may contain `=` |
| `04-py-two-modules` | feature | Add a function in one module and use it from another |
| `05-go-dedupe` | refactor | Extract duplicated logic. Passing tests is not enough — the duplicate expression must actually be gone |
| `06-py-sqlite-bug` | fix | An inner join that drops users with no orders |
| `07-go-error-swallow` | fix | `n, _ := strconv.Atoi(s)` |
| `08-go-race` | concurrency | A data race. Only `go test -race` sees it, so a harness that runs plain `go test` will believe it finished |
| `09-explore-answer` | explore | Nothing to fix. Find the function that decides token validity and write its name to `ANSWER.txt` |
| `10-go-api-rename` | api-change | Rename a method across three files. Deleting the caller instead is caught |
| `11-py-regression` | regression | A collapse-runs regex regressed to a single-character one |
| `12-go-nil-guard` | fix | Writing to a nil map, which panics rather than failing an assertion |

Go, Python and SQL; bug fixes, a feature, a refactor, a rename, a concurrency bug, test authorship
and pure exploration. Every fixture is under fifty lines and needs no network.

### Every pass condition is proven to discriminate

    bench/validate-tasks.sh

For each task this copies the fixture twice: `verify.sh` must **fail** on the untouched fixture, and
must **pass** after `oracle.sh` applies a known-good fix. A task that starts solved, or that cannot
be solved, is a bug in the benchmark rather than a hard task. All thirteen pass both checks.

The oracles live beside the tasks and are never copied into a run — the runner copies `repo/` and
nothing else, so a harness cannot read the answer.

## Reproducing it

You need the model pulled and a server on an OpenAI-compatible endpoint. Nothing else: no API key,
no account, no spend. That is deliberate — a benchmark nobody can rerun is a press release.
