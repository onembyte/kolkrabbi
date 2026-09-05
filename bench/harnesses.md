# Harness configuration

The exact command line and configuration used for every harness, so that a disagreement about the
result can be turned into a disagreement about the configuration, which is a fixable kind.

## Kolkrabbi — verified

    kolk --base-url <URL> -m <MODEL> -P full-auto -p "<PROMPT>"

- `-P full-auto` so the run is not blocked on a permission prompt. The hardline blocklist still
  refuses; see `docs/plan/13-tools-permissions-sandboxing.md`.
- `-p` is single-shot: one request, the tool loop runs to completion, then the process exits.
- `--base-url` takes any OpenAI-compatible endpoint and sends no key.

## Codex CLI — **UNVERIFIED**

    codex exec --skip-git-repo-check "<PROMPT>"

with `~/.codex/config.toml` pointing at the local server:

    model = "<MODEL>"
    model_provider = "ollama"
    [model_providers.ollama]
    name = "Ollama"
    base_url = "http://127.0.0.1:11434/v1"
    wire_api = "chat"

Written from Codex's published configuration reference on 2026-09-04, **not from a run**. `ollama`
is a reserved built-in provider id. Correct this file the first time it is actually exercised.

## OpenCode — **UNVERIFIED**

    opencode run "<PROMPT>"

with the local provider configured in `opencode.json` per their providers documentation. Also
written from published docs, not from a run.

## Claude Code — excluded from the local track

It cannot run a local model: its deployment paths are the Anthropic API, Console, Amazon Bedrock,
Claude Platform on AWS, Google Cloud's Agent Platform and Microsoft Foundry, all of which serve
Claude models. Verified against `code.claude.com/docs` on 2026-09-03. It belongs in the later paid
track, not this one.
