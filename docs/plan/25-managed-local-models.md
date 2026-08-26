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

