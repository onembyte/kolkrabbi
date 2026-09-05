#!/usr/bin/env bash
set -euo pipefail
sed -i.bak 's/re.sub(r"\[^a-z0-9\]", "-", s)/re.sub(r"[^a-z0-9]+", "-", s)/' slug.py && rm -f slug.py.bak
