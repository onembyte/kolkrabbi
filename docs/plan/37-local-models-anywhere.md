# 37. Local models anywhere — this machine, the LAN, or one exact address

Status: drafted 2026-09-09 · supersedes: — · PLAN.md item 37 · checkpoints V39.x

## Decision (the short version)

Kolkrabbi can already talk to a local model. It finds one Ollama, on one address, and that
address is `127.0.0.1:11434` written into `cli.go`. A model on the desktop in the next room, or
on a rented GPU box reached over Tailscale, cannot be used at all — not because the plumbing
cannot carry it (`local.NewHostBackend` takes any address; the route map is keyed by a name)
but because nothing can say where it is.

**Adopt one idea, the local endpoint: a name, an address, and whatever answers there.** The
three reaches the owner asked for stop being three features:

| Reach | What the user types |
| --- | --- |
| this machine | nothing — the Ollama on loopback is still found by itself, and is now named `ollama` |
| a box on the LAN | `/localia add shop 192.168.1.50:11434` |
| one exact address, Tailscale included | `/localia add rig 100.64.0.7:11434` |

An endpoint is probed when it is added, and again when a session starts. The probe says what is
there rather than assuming: an Ollama answers its own handshake, and anything OpenAI-compatible
answers `GET <base>/models` with a list. That second case is what makes Docker Model Runner,
llama.cpp's server, LM Studio and vLLM work without a line of code each.

The model id carries the endpoint: `shop/qwen3-27b` runs on the shop box, `ollama/qwen3` runs
here. The route map is already keyed that way, so `/model`, the picker, the slots and the effort
dial need no new concept — an endpoint is simply another place models come from.

## Spec

### 1. The endpoint record

```
local.endpoints: [ { name, addr, kind, base } ]   kind: ollama | openai
local.endpoint:  the name new sessions start on ("" = the gateway, as today)
```

`name` is one word, `[a-z0-9-]`, unique, and is the model-id prefix. `addr` is `host:port`, or a
full `scheme://host:port/base` when the runtime does not serve at the root. `kind` and `base` are
what the probe found, saved so a session start does not have to re-derive them; a probe that
disagrees on session start wins and rewrites them.

`ollama` is a reserved name for this machine's own Ollama, discovered as it is today. It needs no
record and cannot be removed, so every id that works now keeps working.

### 2. The probe

One function, `local.Identify(ctx, addr)`, answering what runs at an address, under a deadline:

1. **Ollama** — `GET /` returns `Ollama is running` and `/api/version` returns a version. Models
   from `/api/tags`; chat at `<addr>/v1`.
2. **OpenAI-compatible** — `GET <base>/models` returns `{"data":[{"id":…}]}`, where `<base>` is
   tried in this order: the address as given, then `/v1`, then `/engines/v1` (Docker Model
   Runner). Chat at whichever answered.
3. **Nothing** — the address is refused with what was tried, never added.

The probe sends no key and no prompt: it asks two questions a model server answers for anyone.

### 3. The commands

```
/localia                        what this machine has, and every endpoint with its state
/localia add <name> <addr>      probe, then save it
/localia rm <name>              forget one
/localia use <name> [model]     point this session at it; with no model, list what it has
/localia use here               back to this machine
/localia direct <command…>      run a model runner on this machine, then use what it serves
```

`use` switches the running session — the model changes the way `/model` changes it — and records
the endpoint so the next session starts there. `/model` keeps working: an id with a known
endpoint prefix routes to it.

### 4. `direct`

`/localia direct docker model run hf.co/orcarouter/Qwen3.8-27B-Uncensored` is the owner's own
words. Kolkrabbi runs that command through the session's shell — under the permission tier, shown
before it runs, exactly like any other command a turn would run — then probes the runner
endpoints it knows (`127.0.0.1:12434` for Docker Model Runner, `11434` for Ollama), and switches
to the model the command named, which is its last argument.

It is the user's command. Kolkrabbi does not write it, does not guess a runner, and does not
install one; a command that fails is reported with its output and nothing is switched.

### 5. What leaves the machine, said once

Adding a non-loopback endpoint prints, once, before it is saved: prompts, file contents and tool
output will go to that host; plain HTTP over a LAN is readable by anything on that LAN, while a
Tailscale address is carried inside its tunnel. `https://` is accepted where a runtime offers it.

No key is ever sent to a local endpoint — the OpenRouter key is bound to openrouter.ai and the
existing test pins that. An endpoint that demands a key is refused with that sentence, because a
local runtime that wants a secret is not what this feature is for.

### 6. Not in scope

Starting a runtime on another machine, installing one anywhere, discovering endpoints by
broadcast or mDNS, and any model management on a remote box: `pull` and `plan` stay about this
machine, where the hardware probe can tell the truth.

## Leaves

- V39.1 — `local.Identify` over both runtimes with a deadline, and the endpoint record; tested
  against fixtures for each shape and against an address where nothing listens.
- V39.2 — the endpoints in config, `/localia add|rm|use|list`, routing by endpoint name, the
  session switch, and the sentence about what leaves the machine.
- V39.3 — `/localia direct <command…>`.
- V39.4 — `kolk doctor`, the README, the site's local-models page and the provider wall.
