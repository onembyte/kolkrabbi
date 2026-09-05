#!/usr/bin/env bash
set -uo pipefail
go test ./... 2>&1 || exit 1
if grep -q "func (s \*Store) Sum()" store.go 2>/dev/null; then
  echo "Sum is still defined; the rename was worked around rather than done"
  exit 1
fi
echo "renamed and tests pass"
