#!/usr/bin/env bash
# "The engine touches no OS."
#
# Asserts that no package above the platform layer has a GOOS-suffixed file, and
# that os/exec and the home-directory lookups have exactly one owner each. This
# is the load-bearing precondition for every constrained target on the roadmap:
# the daemon, the iPad, and any gomobile build.
set -euo pipefail
cd "$(dirname "$0")/.."
exec go test ./internal/arch/ -count=1 -run 'TestOnlyThePlatformLayerHasOSFiles|TestOSAccessHasOneOwner|TestNoRuntimeGOOSBranching' "$@"
