#!/usr/bin/env bash
# Layering, dependency and naming rules.
#
# The rules are data in internal/arch/layers.go and are enforced by Go, not by
# grep — an AST knows the difference between a rule name in a comment and a call
# to it. This script exists so the gate has a name you can type.
set -euo pipefail
cd "$(dirname "$0")/.."
exec go test ./internal/arch/ -count=1 "$@"
