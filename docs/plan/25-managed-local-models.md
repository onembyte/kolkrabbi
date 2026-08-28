# 25. Managed local models

Status: contract checkpoint · 2026-08-26

Kolkrabbi may provide local models without using an Ollama installation or
service owned by the host. The local backend is a Kolk-managed, versioned
Ollama sidecar with a private endpoint and a model store inside Kolk's data
directory.

## Contract

- Kolk downloads or receives explicit user approval before installing a
  versioned sidecar binary. It verifies the binary before execution.
- Kolk starts the sidecar itself, gives it a Kolk-owned model directory and
  private listen address, and shuts it down with the Kolk session.
- Kolk never discovers, starts, stops, or connects to a host Ollama service.
- Model blobs, manifests, quantization metadata, and download state remain
  below Kolk's managed local-model directory. No model is pulled implicitly.
- `/localia` is the interactive entry point for hardware status, storage
  usage, model catalog, pull approval, GPU configuration, and local-model
  selection. Every pull is an explicit user action.

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

## L13.6 — what the pin actually needs, measured 2026-08-28

`pinnedRuntime` was left empty for review, and L13.5b4 was described as a task
with no code in it: pick a release, verify it, record three values. That was
wrong, and the release data says so. The install path cannot accept any shape
Ollama publishes, so there is nothing to review until it can.

The numbers below are from `ollama/ollama` v0.33.1 and three older tags, read
from the GitHub release API rather than assumed.

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

### The four things that block a pin

1. **Extraction does not exist.** `InstallRuntime` writes downloaded bytes to
   the destination, marks them executable and renames. Every asset is an archive.
2. **The size bound contradicts its subject.** `maxRuntimeBytes` is 1 GiB and its
   comment reads "a managed inference runtime is tens of megabytes". The linux
   asset is 1.42 GB, and the bare binary was already 304 MB in v0.1.32. The
   comment was never true, not merely outdated.
3. **One pin cannot serve four platforms.** `PinnedRuntime()` returns a single
   URL and digest; nothing near it reads `GOOS`/`GOARCH`.
4. **The idempotence check breaks under extraction.** `runtimeMatches` hashes the
   *installed binary* against the *pinned* digest. Once the pin is an archive's
   digest those are different values, so a correct install would be re-downloaded
   every time — 1.4 GB per run.

### The decision this needs before any of it is built

Managed installation now means a **>1 GB download on Linux**, plus zstd, which
the Go standard library does not have and which the two-module dependency gate
will not buy. That is a different feature from the one plan 25 described, and it
should be re-chosen rather than inherited.

- **A — shell out for zstd on Linux, stdlib for macOS and Windows.** Keeps the
  dependency gate. Adds a host-tool requirement that has to be detected up front
  and explained, not discovered mid-install. Shelling out is already how the
  provider CLIs work, so it is not a new kind of thing.
- **B — take a zstd dependency.** Buys symmetry and costs the gate that has held
  at two modules. Refused unless the owner moves the gate deliberately; it is not
  a decision to make by `go get`.
- **C — keep `localia`'s reporting and planning, drop managed installation.**
  Hardware probing, the model catalogue and fit plans are built and useful on
  their own. Only *installing the runtime* is blocked. Given 1.4 GB, this
  deserves real weight rather than being the fallback.
- **D — pin an old release that still shipped `.tgz`.** Rejected here: pinning an
  eighteen-month-old runtime to avoid a compression format is choosing the wrong
  thing to be stable about.

**Recommendation: decide between A and C before building.** B is a gate change
and D is a trap. The rest of L13.6 is written assuming A, and is wasted work
under C — which is exactly why the decision is its own leaf and comes first.

### If A is chosen

Extraction must inherit the discipline `internal/selfupdate/artifact.go` already
proves out — allowlisted member paths, regular files only, per-member and total
expansion caps, no extended metadata, nothing executed before its bytes are
checked. That code is the model, not the library: it is gzip-only and holds the
whole archive in memory, which is right for a 10 MB kolk release and impossible
for a 1.4 GB one. Streaming is not an optimisation here, it is the requirement.

Two things the current path never had to think about, both of which arrive with
the size: **peak disk** is the archive plus its expansion, roughly 3 GB, and it
should be checked before the download rather than discovered at 90%; and a
**resumable or at least restartable** download, because a 1.4 GB transfer that
fails at the end and starts over is a feature people turn off.
