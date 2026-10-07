# Localia: local models in Kolk

Open a local session without a remote API key:

```sh
kolk -m ollama/qwen2.5-coder:7b
```

Inside that session:

```text
/localia setup
/localia models
/localia plan qwen2.5-coder:7b
/localia pull --yes qwen2.5-coder:7b
/model ollama/qwen2.5-coder:7b
```

`setup` installs and starts the native runtime when needed. An approved pull or local model
selection also prepares a missing runtime. Installation uses Kolk's user storage; it needs no
Docker, sudo, external decompressor, or handwritten configuration. Opening the model picker,
listing models and viewing status do not install or start anything.

Model downloads remain explicit. `plan` shows the fit estimate; `pull` asks before downloading
in the plain terminal, and `--yes` supplies that answer in the TUI. The fit catalog lists the
variants Kolk can currently size before a pull. Already cached custom models and namespaces
also appear in the picker. A cached tag is exact: having `7b` does not imply `14b` is present.
If the runtime has not identified a custom model's execution location, its row says unknown;
a cached manifest alone does not prove it runs locally or costs nothing.

An Ollama Cloud tag (`:cloud`, or an explicit tag ending in `-cloud`) has no fit plan: it runs
on ollama.com, and `/localia pull --yes gpt-oss:120b-cloud` pulls its manifest through the
session's server once that server confirms a remote host; an unconfirmed name is refused. The
server must be signed in (`/plans login ollama <plan>`). A picker id such as
`ollama/qwen2.5-coder:7b` is accepted with its prefix.

## Runtime lifetime

```text
/config set local.ephemeral on
/config set local.ephemeral off
/config get local.ephemeral
/localia stop
```

This setting belongs to the current project and takes effect in its next session.

| Setting | When the session closes |
| --- | --- |
| `on` (default) | Kolk stops its own runtime. Follow-up tasks in the session reuse it. |
| `off` | The project runtime keeps running and later sessions can reuse it. |

Downloaded runtime files and models stay cached in both cases. Turning `local.ephemeral` back
on does not stop a runtime an earlier `off` left running: sessions reuse it instead of
starting a second one, `/localia` and `/doctor` say it was kept, and it keeps running until
`/localia stop` explicitly stops it or its process ends (for example at a restart). The stop
command verifies the recorded process identity, its Ollama response, and ownership of its
recorded loopback listener before signalling it; if those checks fail, it leaves the process
alone. If that kept process runs but stops answering, a session starts its own beside it and
says so; with `off`, the project's one runtime is waited on instead, and local models fail until
it answers or its process ends. Kolk never signals a process another session started unless
`/localia stop` is explicitly requested. A server you started separately remains yours; Kolk
uses it and leaves it running. Pulls, chat, model discovery and sign-in use
the session's same endpoint. An approved pull or `/plans login ollama <plan>` starts an installed,
idle runtime; with no runtime yet, sign-in asks for `/localia setup` first. `/localia` shows its
configured lifetime and current state.

## Installation and recovery

Kolk discovers the latest stable official Ollama release when installation is needed. It checks
the exact asset origin, SHA-256 digest and size, extracts into private temporary storage, and
publishes a completed installation. Companion programs, libraries and notices stay together.
Concurrent projects share one installation attempt; completed installations are reused offline.
Intel and Apple Silicon builds keep separate completion records, so switching between a
Rosetta build and a native build does not invalidate the other.

Cancellation removes that attempt's staging data. A completed directory left by an interrupted
publication can be recovered without downloading the bundle again. A damaged completion record
produces an error instead of replacing a directory a running process may be using.

Weights use Ollama's existing model store (`OLLAMA_MODELS`, or its usual user directory).
Runtime lifetime never deletes those weights. Local sessions can be resumed with `kolk -r`
without adding a remote key. A later switch to a remote model checks that provider's
credentials and retains an explicit `--base-url` from startup.

## Platform scope

- Managed native setup: macOS 14 or newer on Intel/Apple Silicon; Linux on amd64/arm64.
- Linux setup installs the standard bundle, which includes CUDA for NVIDIA GPUs. As the official
  installer does, it adds the ROCm bundle for an AMD GPU on amd64 (not when the NVIDIA driver is
  loaded, which serves an NVIDIA card beside it) and the JetPack 6 or 5 bundle for a Jetson on
  L4T R36 or R35. The pull question names that bundle before a yes. The bundle
  is verified like the runtime and installed as a new tree, never into one in use. A runtime
  installed earlier gains it the next time Kolk starts the runtime (local setup, a pull or a
  model start); one already running, such as a persistent runtime, keeps running without it
  until it stops. A sign-in never downloads a runtime or bundle. If the bundle cannot be
  added, the working runtime keeps running and setup says why; on a first install the runtime
  is installed without it, as the official installer would leave it. When adding the bundle to
  an installed runtime fails in what was downloaded (a checksum mismatch, an archive that will
  not unpack or merge), the failure is remembered, so later sessions do not download that
  release again; `/localia` shows why, and a new Ollama release is tried automatically. A
  first install does not remember one: the next start downloads that release again to add the
  bundle, and remembers a second failure. `/localia setup` forgets remembered failures and
  retries at once when no runtime is running; otherwise the next start of the runtime retries.
  Running out of disk or a network failure is never remembered. MLX is published but not installed,
  matching the official installer. `/localia` names a missing bundle, and hardware no official
  bundle serves (an older JetPack, an AMD GPU on arm64), rather than using it silently.
  Existing separately configured runtimes can still be adopted and are never changed.
- Windows managed installation and persistent runtime locking are not implemented.
- Hardware fit figures are estimates. Fixture tests and cross-builds do not establish physical
  GPU performance; actual release and machine trials are recorded in `CHECKPOINTS.md`.

The installer follows the official [Linux packaging](https://github.com/ollama/ollama/blob/main/scripts/build_linux.sh)
and [macOS packaging](https://github.com/ollama/ollama/blob/main/scripts/build_darwin.sh).
The vendor's current requirements are documented for [Linux](https://docs.ollama.com/linux)
and [macOS](https://docs.ollama.com/macos).
