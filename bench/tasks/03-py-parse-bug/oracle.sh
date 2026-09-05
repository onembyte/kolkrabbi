#!/usr/bin/env bash
set -euo pipefail
sed -i.bak 's/line.split("=")/line.split("=", 1)/' kv.py && rm -f kv.py.bak
