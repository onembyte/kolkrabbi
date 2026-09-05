# Pilot, 2026-09-05: three coding agents, one local 8B model, 0 of 18

Not a harness benchmark. A pilot, run before committing hours of machine time to the full matrix,
to check that the model could drive an agent loop at all. It could not.

**Machine:** 16 GB Apple Silicon. **Server:** Ollama. **Model:** `llama3.1:8b` (Q4_K_M).
**Harnesses:** Kolkrabbi v1.3.1, Codex CLI 0.151.0, OpenCode 1.18.29.
Three tasks x three harnesses x two runs.

| Harness | 01 off-by-one | 03 python parse bug | 09 find a function | Total |
|---|---|---|---|---|
| Kolkrabbi | 0/2 | 0/2 | 0/2 | **0/6** |
| Codex CLI | 0/2 | 0/2 | 0/2 | **0/6** |
| OpenCode | 0/2 | 0/2 | 0/2 | **0/6** |

Wall time: 2 s minimum, 21 s median, 83 s maximum. Nothing timed out. **One run out of eighteen
modified a file at all.**

Task 01 is a `for` loop that stops one element early, with a failing test that names the function.
Task 09 asks only to find a function and write its name into a file — no code change at all.

## How each one failed

Three independent agent loops, three different shapes of nonsense, one cause.

**Kolkrabbi** received tool calls as message *text* rather than as tool calls, with `"parameters"`
where the schema says `"arguments"`, and invented absolute paths.

**OpenCode** received hallucinated Python: `from todowrite import todowrite`.

**Codex CLI** received invented tool names — `create_goal`, `web_search`,
`multi_agent_v1.wait_agent` — none of which it offers.

The single run that wrote anything is the most instructive. On task 09, against a fixture that is
entirely Go, the model claimed to read
`api_gateway_service/api_token_utils.py` — a file that does not exist, in a language the repository
does not contain — then wrote `is_token_valid` into `ANSWER.txt` and reported success. The correct
answer was `checkExpiry`. It did not fail to find the function so much as decline to look.

## Two models were tried, and one advertises something it does not do

**`qwen2.5-coder:7b`** lists `tools` under Capabilities in `ollama show`. Asked to call a single
trivial tool it returns the call as message content instead, on **both** endpoints:

    # POST /v1/chat/completions   (OpenAI-compatible)
    tool_calls: None
    content: {"name": "read_file", "arguments": {"path": "go.mod"}}

    # POST /api/chat              (Ollama native)
    tool_calls: None
    content: {"name": "read_file", "arguments": {"path": "go.mod"}}

Identical on both, so this is the model's template, not Ollama's compatibility layer.

**`llama3.1:8b`** does return a correct native `tool_call` — for one simple tool, in isolation:

    tool_calls: [{"function": {"name": "read_file", "arguments": "{\"path\":\"go.mod\"}"}}]

It stops doing so once a real agent's system prompt and full tool schema are in front of it, which
is the only condition that matters.

## What this shows, and what it does not

It shows that **an 8B model on 16 GB cannot drive these agent loops**, and that the failure is
uniform across three independently written harnesses. Anyone planning to run a coding agent entirely
locally should know that before buying hardware for it.

It shows **nothing about the harnesses**. When every harness scores zero, the score is measuring the
model. That is exactly why this is published as a pilot and not as a result, and why the harness
comparison waits for a model capable enough that the agent loop is what varies.

## Reproducing it

    ollama pull llama3.1:8b
    bench/run.sh --harness kolk --task 01-go-off-by-one --runs 2 \
      --base-url http://127.0.0.1:11434/v1 --model llama3.1:8b

Raw JSONL and every transcript are in `pilot-2026-09-05/`. Harness configurations are in
[`../harnesses.md`](../harnesses.md); tasks and their machine-checked pass conditions were tagged
`kolkbench-tasks-v1` before any of this ran.
