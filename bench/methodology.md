# KolkBench methodology

**Status: tasks pre-registered, no comparative run has happened yet.** Nothing in `results/` yet.

## What this measures

The **harness**, not the model.

Almost every benchmark you can find answers "which model is smartest". That is not the question this
one asks. Give three coding agents the *same model*, the *same repository* and the *same prompt*, and
they still perform differently — because what varies is the agent loop: how it explores a repository,
when it decides it is finished, how it recovers from a failed edit, what it keeps in context, and how
many tool calls it burns getting there.

That number is not published anywhere we could find. It is also the only axis on which a small
project can honestly compete with a vendor's own client.

## How the model is held constant

All harnesses are pointed at **one local model served over one OpenAI-compatible endpoint**. Not
"the same model family", not "comparable models" — the same weights, the same server, the same
machine, for every run.

This is possible because Kolkrabbi, Codex CLI and OpenCode can all take a `base_url`. It has three
useful consequences:

- **No API spend**, so the run can be repeated as often as needed.
- **No credentials**, so anybody can reproduce it.
- **The model genuinely cannot vary between harnesses**, which is the whole point.

**Claude Code is excluded from this track** because it cannot run a local model at all. That is a
finding, not an omission, and it is reported as one. A separate paid track can compare Kolkrabbi,
Claude Code and OpenCode on one Claude model later; it is not part of the first publication.

## Task format

    bench/tasks/<nn>-<slug>/
      task.toml     the prompt, verbatim and identical for every harness, plus a timeout
      repo/         a small fixture repository
      verify.sh     exit 0 means the task passed

Every pass condition is **machine-checked**. Nothing is scored by reading the output and forming an
opinion. `verify.sh` runs the fixture's own tests, or greps for a required artefact, and the exit
code is the whole result.

Fixtures deliberately need **no network and no dependency installation** — stdlib test runners only
(`go test`, Python `unittest`, `node:test`). A task that needs `npm install` measures your
connection, not the harness.

## Controls against the obvious bias

This benchmark is written by the author of one of the harnesses. That is a real problem and it is not
solved by promising to be fair. These are the mechanisms that let you check instead of trusting:

1. **Pre-registration.** Tasks and pass conditions are committed and **git-tagged before any
   comparative run**. The tag is in the repository history. If a task had been tuned after seeing a
   result, the tag would have to move, and moving it is visible.
2. **Five runs per task.** Agents are stochastic. A single run is an anecdote. Results are reported
   as a pass *rate* with its spread, never as one number.
3. **Raw output published.** Full transcripts and the per-run JSONL, not a summary table. If the
   summary disagrees with the raw data, the raw data is right and the summary is a bug.
4. **Every harness's configuration published**, in `harnesses.md`, including the exact command line.
5. **One prompt, verbatim.** The same string goes to every harness. No Kolkrabbi-shaped phrasing, no
   per-harness "tuning" of the ask.
6. **Losses first.** Any task where Kolkrabbi does worse is reported before the tasks where it does
   better, in the summary and in the article.

## What this does not measure

- **Not model quality.** The model is a constant, chosen for being small enough to iterate on.
- **Not performance on real repositories.** These fixtures are tiny. Behaviour on a 200k-line
  codebase is a different question that this cannot answer.
- **Not general capability.** Twelve tasks on one machine, one operating system, one model.
- **Not cost in production.** The local model is free; the cost columns exist to compare *efficiency*
  between harnesses, not to predict anybody's bill.

## How to attack this benchmark

Written against ourselves, so it is on the record:

- **The author picked the tasks.** Even with pre-registration, task *selection* can favour a harness
  whose strengths the author knows. The mitigation is that fixtures and prompts are public: propose
  a task, and if it is machine-verifiable it goes in.
- **One model is not models.** A harness that suits a small local model may not suit a frontier one.
  The paid track exists to test that, and until it runs, this result should not be generalised.
- **Configuration is a judgement call.** Every harness has knobs. We publish ours; if a harness is
  configured badly, that is a legitimate objection and a fixable one.
- **Five runs is few.** It is enough to see a spread and not enough for a confidence interval. Do not
  read a 3-of-5 versus 4-of-5 as a difference.
- **Small fixtures reward small behaviour.** An agent that explores carefully is penalised on a
  20-line repository and rewarded on a real one. This benchmark structurally favours the terse.

## Metrics recorded per run

| Field | Meaning |
|---|---|
| `task`, `harness`, `harness_version`, `model`, `run` | what was run |
| `passed`, `verify_exit` | the machine-checked result |
| `wall_seconds` | start to exit |
| `timed_out` | whether the timeout killed it |
| `files_changed`, `insertions`, `deletions` | measured with `git diff --numstat` against the fixture baseline |
| `transcript` | path to the harness's full output |

Cost and token counts are recorded where the harness reports them. They are zero for a local model
by definition; the column is there for the later paid track.
