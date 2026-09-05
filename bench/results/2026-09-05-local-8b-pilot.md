# Pilot, 2026-09-05: no 8B local model drove any of the three harnesses

Not a benchmark result. A pilot, run before committing hours of machine time, on the **easiest task
in the set** — `01-go-off-by-one`, a loop that stops one element early.

Machine: 16 GB Apple Silicon. Server: Ollama. One run per harness.

| Harness | Version | Result | How it failed |
|---|---|---|---|
| Kolkrabbi | v1.3.1 | fail, 31 s, 0 files | Model emitted tool calls as **text**, with `"parameters"` instead of `"arguments"`, and invented absolute paths |
| OpenCode | 1.18.29 | fail, 33 s, 0 files | Model hallucinated a Python API — `from todowrite import todowrite` |
| Codex CLI | 0.151.0 | fail, 20 s, 0 files | Model invented tool names wholesale — `create_goal`, `web_search`, `multi_agent_v1.wait_agent` |

Nothing was edited by any harness. Three different agent loops, three different shapes of nonsense,
one cause.

## Two models were tried

**`qwen2.5-coder:7b`** advertises tool support — `ollama show` lists `tools` under Capabilities — and
does not deliver it. Asked to call a single trivial tool, it returns the call as message content on
**both** Ollama's OpenAI-compatible `/v1/chat/completions` and its native `/api/chat`:

    tool_calls: None
    content: {"name": "read_file", "arguments": {"path": "go.mod"}}

Since both endpoints behave the same way, this is the model, not Ollama's compatibility layer.

**`llama3.1:8b`** does emit a correct native `tool_call` for a single simple tool. It stops doing so
once a real agent's system prompt and full tool schema are in front of it, which is the only
condition that matters.

## What this does and does not show

It shows that **an 8B model on this hardware cannot drive an agent loop** on a real repository task,
and that the failure is uniform across three independent harnesses. That is a useful thing to know
before buying a GPU to run a coding agent locally.

It shows **nothing about the harnesses**. When every harness scores zero, the score is measuring the
model. Any harness comparison needs a model capable enough that the differences between agent loops
are what varies — which is why the comparative run uses a capable model, and why this pilot is
published separately rather than as a result.

The honest short version: local models are excellent for chat and completion at this size, and are
not yet agents.
