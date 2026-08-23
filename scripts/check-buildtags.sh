#!/usr/bin/env bash
# The silently-wrong-build gate.
#
# `unix` is not a GOOS. A file named shell_unix.go with no //go:build line
# compiles on Windows too — verified on go1.26.4 — and the result is a wrong
# binary, not a compile error. _other, _posix, _stub and _generic are equally
# decorative. Hence its own named gate.
set -euo pipefail
cd "$(dirname "$0")/.."
exec go test ./internal/arch/ -count=1 -run 'TestOSFilesDeclareTheirBuildConstraint' "$@"
