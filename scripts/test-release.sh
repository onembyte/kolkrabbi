#!/usr/bin/env bash
# Static release contract. Snapshot artifacts get a separate executable test.
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CONFIG="$ROOT/.goreleaser.yaml"
MAKEFILE="$ROOT/Makefile"
CI="$ROOT/.github/workflows/ci.yml"
failures=0
checks=0

pass() { checks=$((checks + 1)); }
fail() { checks=$((checks + 1)); failures=$((failures + 1)); printf 'release: %s\n' "$1" >&2; }

contains() {
  local text="$1" label="$2"
  if [ -f "$CONFIG" ] && grep -Fq -- "$text" "$CONFIG"; then pass; else fail "$label"; fi
}

excludes() {
  local pattern="$1" label="$2"
  if [ -f "$CONFIG" ] && ! grep -Eq -- "$pattern" "$CONFIG"; then pass; else fail "$label"; fi
}

contains_file() {
  local file="$1" text="$2" label="$3"
  if [ -f "$file" ] && grep -Fq -- "$text" "$file"; then pass; else fail "$label"; fi
}

contains 'version: 2' "GoReleaser v2 schema is not pinned"
contains 'project_name: kolk' "release project name is not kolk"
contains 'main: ./cmd/kolk' "release does not build cmd/kolk"
contains 'binary: kolk' "archive binary is not named kolk"
contains 'CGO_ENABLED=0' "release build does not disable cgo"
contains '- -trimpath' "release build does not remove host paths"
contains 'internal/buildinfo.version={{.Version}}' "release version is not stamped"
contains 'internal/buildinfo.commit={{.FullCommit}}' "release commit is not stamped"
contains 'internal/buildinfo.date={{.Date}}' "release date is not stamped"
contains 'goos: [darwin, linux]' "release OS set must be exactly Darwin and Linux"
contains 'goarch: [amd64, arm64]' "release architecture set must be amd64 and arm64"
excludes 'windows|formats: \[zip\]' "Windows must not ship before its runtime checkpoint"
contains 'name_template: "kolk_{{ .Version }}_{{ .Os }}_{{ .Arch }}"' "archive names are not deterministic and versioned"
contains 'formats: [tar.gz]' "release archives are not tar.gz"
contains 'name_template: checksums.txt' "checksum manifest name drifted"
contains 'algorithm: sha256' "checksum algorithm is not explicit SHA-256"
contains 'cmd: cosign' "checksum manifest is not signed with Cosign"
contains 'signature: "${artifact}.sigstore.json"' "Cosign v3 bundle name is missing"
contains '"--bundle=${signature}"' "Cosign does not emit a verification bundle"
contains 'artifacts: checksum' "signature is not bound to the checksum manifest"
contains 'version_template: "1.3.1-dev.{{ .ShortCommit }}"' "snapshot version does not match the current release line"
contains 'draft: false' "release would be left as a draft"
contains_file "$MAKEFILE" 'release-check:' "Makefile has no release-check target"
contains_file "$CI" 'run: make release-check' "CI does not enforce the release contract"

if [ "$failures" -ne 0 ]; then
  printf 'release: %d/%d checks failed\n' "$failures" "$checks" >&2
  exit 1
fi
printf 'release: %d checks passed\n' "$checks"
