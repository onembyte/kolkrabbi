#!/usr/bin/env bash
set -euo pipefail
sed -i.bak "s/i < len(xs)-1/i < len(xs)/" sum.go && rm -f sum.go.bak
