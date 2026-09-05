# Harness configuration

The exact command line and configuration used for every harness, so that a disagreement about the
result can be turned into a disagreement about the configuration, which is a fixable kind.

## Kolkrabbi — verified

    kolk --base-url <URL> -m <MODEL> -P full-auto -p "<PROMPT>"

- `-P full-auto` so the run is not blocked on a permission prompt. The hardline blocklist still
  refuses; see `docs/plan/13-tools-permissions-sandboxing.md`.
- `-p` is single-shot: one request, the tool loop runs to completion, then the process exits.
- `--base-url` takes any OpenAI-compatible endpoint and sends no key.

## Codex CLI — verified against codex-cli 0.151.0, 2026-09-05

    CODEX_HOME=<throwaway> codex exec --skip-git-repo-check "<PROMPT>" </dev/null

with a `config.toml` that uses the **built-in** provider and defines no custom block:

    model = "<MODEL>"
    model_provider = "ollama"
    approval_policy = "never"
    sandbox_mode = "workspace-write"

**Correction, and it is worth recording.** This file first carried a custom
`[model_providers.ollama]` block with `wire_api = "chat"`, written from the published configuration
reference. Codex 0.151.0 rejects that outright:

    Error loading config.toml: `wire_api = "chat"` is no longer supported.
    How to fix: set `wire_api = "responses"` in your provider config.

Ollama does not implement the Responses API, so `responses` is not an option either. The path that
works is the **reserved built-in `ollama` provider id**, with no custom block at all. The published
docs describe a configuration this version refuses.

`</dev/null` matters: `codex exec` reads stdin even when the prompt is an argument, and will sit
waiting without it.

## OpenCode — verified against opencode 1.18.29, 2026-09-05

    XDG_CONFIG_HOME=<throwaway> opencode run "<PROMPT>"

with `<throwaway>/opencode/opencode.json` declaring the local endpoint through
`@ai-sdk/openai-compatible`. See `bench/config/opencode/opencode.json`.

`XDG_CONFIG_HOME` is what keeps a run out of the operator's own `~/.config/opencode`. Configuring
the provider in the fixture directory instead would have put a config file inside the repository the
agent is editing, which would show up in the diff and corrupt the `files_changed` metric.

## Claude Code — excluded from the local track

It cannot run a local model: its deployment paths are the Anthropic API, Console, Amazon Bedrock,
Claude Platform on AWS, Google Cloud's Agent Platform and Microsoft Foundry, all of which serve
Claude models. Verified against `code.claude.com/docs` on 2026-09-03. It belongs in the later paid
track, not this one.
