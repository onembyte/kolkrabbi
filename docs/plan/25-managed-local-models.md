# 25. Local models through a host Ollama

Status: contract rewritten as option E · 2026-08-29 · supersedes the managed-sidecar contract of 2026-08-26

Kolkrabbi uses the Ollama the user already has. It finds a running server on
the loopback default, or starts one of its own on a port it chooses when the
binary is on PATH and nothing is listening. Its models appear in the picker
with no configuration; Ollama Cloud models appear once the user has signed in,
and the only thing the user is asked to do is that sign-in.

## Contract

- Kolk never installs Ollama. When the binary is absent, `kolk doctor` and the
  picker name the one command that installs it; kolk does not run it, because
  kolk's own hardline forbids `curl | sh` and `sudo`, and a floor that the
  product itself steps over is not a floor.
- Kolk probes the literal `127.0.0.1:11434` and nothing else — never
  `OLLAMA_HOST`, which may point at another machine or at ollama.com. A server
  found there is **adopted read-only**: used, never stopped. On this machine
  that server is the user's own shell process; on a systemd install it is the
  service whose key holds the sign-in. Both are somebody else's.
- When nothing listens and `ollama` is on PATH, kolk starts `ollama serve` on a
  loopback port **it chooses**, with a curated environment (`HOME`, `PATH`,
  `OLLAMA_HOST`; never `OPENROUTER_API_KEY`), waits for readiness, says so once
  in the transcript, and stops **only what it started** — with a death signal
  on Linux and a job object on Windows, because a server that outlives kolk on
  `SIGTERM` becomes a "host server" the next session must never touch.
- The model store is the host's. Plan 25's private store is dropped: a
  kolk-started server that cannot see the user's pulled models defeats the
  point of listing them. No model is pulled implicitly; a pull is an explicit
  approval against the host's `/api/pull`.
- Host models are **rows in the picker and never the zero-config default.**
  B12.13's free-first order cannot tell a 1.5B local model from a 480B free
  gateway one, so local is an explicit choice. A `routing.prefer_local` may
  come later, with the same never-surprises discipline as its siblings.
- The backend is HTTP against the local server's OpenAI-compatible `/v1`, not
  a CLI adapter: kolk runs its own tool loop and sends tool schemas every turn,
  and the vendor CLI has neither. Cloud models go through the **same** local
  server, which signs upstream requests with its own key; kolk never holds a
  credential for ollama.com.
- The cloud connector is verified by `POST /api/me` on the local server — `200`
  with the plan, or `401` with a `signin_url` kolk prints — **not** by a first
  answered turn. A local model answering proves nothing about a sign-in, and a
  verifier that cannot tell the two apart would make Ollama Cloud the session
  default for someone who never signed in.
- Host models carry a backend prefix (`ollama/<name>`) that a router in the
  engine owns and strips at the wire. The gateway's catalogue never holds a
  host id and the router never sends a host id to the gateway. A persisted host
  id whose server is gone is a stop that names the server, never a silent
  route to OpenRouter.


## GPU and storage policy

Kolk reports detected accelerator type, VRAM, system RAM, and available disk
space, but never assumes that all detected capacity is safe to consume.
Defaults reserve headroom for the operating system and Kolk. The user may
choose GPU offload mode and a model quantization/storage variant before a
pull. The selected settings are persisted as non-secret local configuration
and are passed to the managed sidecar only when supported by that sidecar
version.

The planner must show the estimated model size, required VRAM/RAM, reserved
headroom, and expected fallback before confirmation. If the selected model
does not fit, Kolk rejects the pull with an actionable explanation rather than
silently degrading or using swap.

The hardware snapshot has a deterministic, probe-independent shape:

```text
accelerators: [{vendor, name, vram_bytes, available_vram_bytes}]
system_ram_bytes
disk_free_bytes
```

Probe adapters may use platform-native sources such as Linux device metadata
or vendor utilities, but they return this shape to the planner and may fail
closed to an "unknown" value. A missing probe never means zero capacity and
never authorizes a pull.

The persisted local configuration uses:

```text
gpu_mode: auto | cpu | gpu
gpu_index: integer (only with gpu mode)
quantization: provider model variant selected by the user
reserved_vram_fraction: [0, 1)
reserved_ram_bytes
```

`auto` selects the largest fitting supported GPU configuration after applying
reserved headroom; it does not consume multiple GPUs unless the user opts in.
Kolk never promises that a quantization variant fits based on file size alone:
the planner compares both storage size and runtime memory requirements.

## TDD checkpoints

1. Contract tests prove sidecar paths, private endpoint ownership, explicit
   pull approval, and host-service isolation.
2. Hardware tests prove deterministic parsing of GPU/RAM/storage probes and
   safe behavior when probes are unavailable.
3. Runtime implementation adds lifecycle and model-store management only after
   those tests pass.
4. Command tests cover `/localia` and CLI parity without requiring a GPU or
   Ollama installation.
5. Verification records race/full-suite results and a manual GPU smoke test
   as separate evidence.

## L13.6 — option E, and why A–D are gone

The managed-sidecar contract asked kolk to download, verify and run its own
Ollama. Measured against the release data on 2026-08-28, that meant a >1 GB
zstd archive per platform, a separate asset per GPU vendor, and an extraction
path this repository does not have. Four options were written up (shell out
for zstd; take a dependency; keep only reporting; pin an old release). The owner
chose none of them and asked the better question: the user already has Ollama,
or can install it in one line — why is kolk shipping a second one?

That is option E. It was reviewed before being adopted (seven agents, checked
against the live 0.33.1 server on this machine and against upstream source;
four refutation passes found nothing false). What the review changed is in the
contract above: adopt-don't-start on the default port, `/api/me` instead of a
spent turn, local never the default, the install line named rather than run,
and a router before a second backend.

### What is actually shipped

| Asset | Size | Archive |
|---|---|---|
| `ollama-linux-amd64.tar.zst` | 1.42 GB | zstd |
| `ollama-linux-amd64-mlx.tar.zst` | 957 MB | zstd |
| `ollama-linux-arm64.tar.zst` | 1.54 GB | zstd |
| `ollama-darwin.tgz` | 159 MB | gzip |
| `ollama-windows-amd64.zip` | 1.46 GB | zip |

A bare `ollama-linux-amd64` executable existed up to **v0.3.0** and was gone by
**v0.5.0**. So this install path was written against a real shape that has since
changed, not an imagined one — which matters, because "it never worked" and "it
stopped working" call for different repairs.

### What the review confirmed, so nobody re-derives it

- Cloud models run **through the local server** with no prior pull when the
  name carries `:cloud` or `-cloud`; `/api/show` proxies cloud names and
  returns `capabilities` and context length; the cloud catalogue is readable
  unauthenticated at `ollama.com/api/tags`.
- `/api/show` has carried `capabilities` (`completion`, `tools`, `vision`,
  `thinking`, …) since v0.6.4; `/v1` has streamed tool calls since v0.8.0.
  Plan 03's note that Ollama omits the tool-call `index` is stale upstream.
- `/v1/models` on an empty server returns `"data": null`, not `[]`.
- A second `ollama serve` on a bound port exits 1 with
  `bind: address already in use`.
- `ollama signin` needs a reachable server, opens a browser, prints the URL and
  returns without polling. On a systemd install the sign-in belongs to the
  service's key (`/usr/share/ollama/.ollama`), not the user's — which is why
  adopting the running service comes before starting one.
- Ollama Cloud's `429` is a session or weekly limit that **resets** (5 h / 7 d);
  it must not be read as A33.7's exhausted plan, which never clears by waiting.
- `/v1` cannot set `num_ctx` per request; the effective window is the server's
  default, and Ollama truncates from the **front**, which drops kolk's system
  prompt and tool schemas first. A kolk-started server sets
  `OLLAMA_CONTEXT_LENGTH`; an adopted one is read from `/api/ps` after load.
- Kolk's 60 s first-byte timeout kills CPU inference: a cold 7B with a 3k-token
  system prompt takes minutes. The Ollama backend needs its own transport.

### The blocker underneath all of it

The engine routes by model id over one backend; only `planBackendFor` and
`switchModel` know a second backend exists. Slot ranking, free rotation, the
fast lane and the default chooser all draw from the one gateway catalogue. A
host id in that slice is a 404 from OpenRouter; a gateway id sent to Ollama is
the same the other way. This is the wall A33.6 hit when it refused
"subscriptions for any slot", and E cannot start until it is gone. The router
is the first leaf for that reason.
