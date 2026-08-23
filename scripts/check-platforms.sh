#!/usr/bin/env bash
# Compile the complete root module for every CLI release target.
#
# A cross-compiled test binary cannot run on the host. `-exec=true` asks the go
# command to build every package and test, then invoke a harmless wrapper in
# place of the foreign binary. Host tests still run normally through
# scripts/test.sh; this gate proves only compilation, and says exactly that.
set -euo pipefail
cd "$(dirname "$0")/.."

targets=(
  darwin/amd64
  darwin/arm64
  linux/amd64
  linux/arm64
  windows/amd64
)

for target in "${targets[@]}"; do
  goos="${target%/*}"
  goarch="${target#*/}"
  echo "── $goos/$goarch ──"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go test -count=1 -run '^$' -exec=true ./...
done
