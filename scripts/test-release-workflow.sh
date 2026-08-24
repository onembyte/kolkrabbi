#!/usr/bin/env bash
# Static and executable contract for the tag-only GitHub release workflow.
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORKFLOW="$ROOT/.github/workflows/release.yml"
TAG_CHECK="$ROOT/scripts/check-release-tag.sh"
MAKEFILE="$ROOT/Makefile"
CI="$ROOT/.github/workflows/ci.yml"
failures=0
checks=0

pass() { checks=$((checks + 1)); }
fail() { checks=$((checks + 1)); failures=$((failures + 1)); printf 'release workflow: %s\n' "$1" >&2; }

contains() {
  local text="$1" label="$2"
  if [ -f "$WORKFLOW" ] && grep -Fq -- "$text" "$WORKFLOW"; then pass; else fail "$label"; fi
}

excludes() {
  local pattern="$1" label="$2"
  if [ -f "$WORKFLOW" ] && ! grep -Eq -- "$pattern" "$WORKFLOW"; then pass; else fail "$label"; fi
}

if [ -f "$WORKFLOW" ]; then pass; else fail ".github/workflows/release.yml is missing"; fi
if [ -f "$TAG_CHECK" ]; then pass; else fail "strict release-tag checker is missing"; fi

contains 'name: release' "workflow name drifted"
contains 'tags:' "workflow has no tag filter"
contains '- "v*"' "workflow trigger is not limited to v* tags"
excludes 'pull_request:|workflow_dispatch:|schedule:|branches:' "workflow has a non-tag trigger"
contains 'permissions: {}' "workflow does not deny permissions by default"
contains 'contents: read' "verify job does not declare read-only contents"
contains 'contents: write' "publish job cannot create a GitHub Release"
contains 'id-token: write' "publish job cannot request a keyless signing identity"
contains 'needs: verify' "publish job is not blocked on verification"
contains "github.repository == 'onembyte/kolkrabbi'" "workflow can publish from an unexpected repository"
contains 'run: make check' "workflow does not run the complete repository gate"
contains 'run: ./scripts/check-release-tag.sh' "workflow does not execute the strict tag guard"
contains 'run: goreleaser check' "workflow does not validate GoReleaser configuration"
contains 'scripts/test-release-snapshot.sh' "workflow does not rehearse the four release archives"
contains 'actions/checkout@d23441a48e516b6c34aea4fa41551a30e30af803 # v6' "checkout action pin drifted"
contains 'actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16 # v6' "setup-go action pin drifted"
contains 'sigstore/cosign-installer@6f9f17788090df1f26f669e9d70d6ae9567deba6 # v4.1.2' "Cosign installer pin drifted"
contains 'cosign-release: v3.0.6' "Cosign binary version is not fixed"
contains 'goreleaser/goreleaser-action@f06c13b6b1a9625abc9e6e439d9c05a8f2190e94 # v7' "GoReleaser action pin drifted"
contains 'version: v2.17.1' "GoReleaser version is not the snapshot-tested release"
contains 'args: release --clean' "publish step does not perform a clean release"
contains "GITHUB_TOKEN: \${{ secrets.GITHUB_TOKEN }}" "publish step does not use the repository token"
excludes 'GH_PAT|PERSONAL_ACCESS_TOKEN' "workflow asks for a broader personal token"
if [ -f "$MAKEFILE" ] && grep -Fq 'release-workflow-check:' "$MAKEFILE"; then pass; else fail "Makefile has no release-workflow-check target"; fi
if [ -f "$CI" ] && grep -Fq 'run: make release-workflow-check' "$CI"; then pass; else fail "ordinary CI does not enforce the workflow contract"; fi

if [ -f "$WORKFLOW" ]; then
  mutable=0
  while IFS= read -r line; do
    ref="${line##*@}"
    ref="${ref%% *}"
    if [[ ! "$ref" =~ ^[0-9a-f]{40}$ ]]; then
      mutable=1
    fi
  done < <(grep -E '^[[:space:]]+uses:' "$WORKFLOW")
  if [ "$mutable" -eq 0 ]; then pass; else fail "workflow contains a mutable action reference"; fi
fi

if [ -f "$TAG_CHECK" ]; then
  if bash -n "$TAG_CHECK"; then pass; else fail "release-tag checker fails bash -n"; fi
  for tag in v1.1.0 v1.2.3 v1.2.3-rc.1 v1.2.3+build.5; do
    if "$TAG_CHECK" "$tag" >/dev/null 2>&1; then pass; else fail "valid tag $tag was rejected"; fi
  done
  for tag in 0.1.0 v01.2.3 v1.02.3 v1.2 v1.2.3/evil v1.2.3- v1.2.3-01 proto-0; do
    if "$TAG_CHECK" "$tag" >/dev/null 2>&1; then fail "invalid tag $tag was accepted"; else pass; fi
  done
fi

if [ "$failures" -ne 0 ]; then
  printf 'release workflow: %d/%d checks failed\n' "$failures" "$checks" >&2
  exit 1
fi
printf 'release workflow: %d checks passed\n' "$checks"
