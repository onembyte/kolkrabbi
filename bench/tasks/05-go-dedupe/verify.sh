#!/usr/bin/env bash
set -uo pipefail
go test ./... 2>&1 || exit 1
n=$(grep -c "strings.ToLower(strings.TrimSpace(" *.go | awk -F: '{s+=$2} END {print s+0}')
if [ "$n" -gt 1 ]; then
  echo "the normalisation expression still appears $n times; it was not extracted"
  exit 1
fi
echo "tests pass and the duplicated expression appears once"
