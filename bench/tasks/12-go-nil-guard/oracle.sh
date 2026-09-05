#!/usr/bin/env bash
set -euo pipefail
python3 - <<'P'
import pathlib
p = pathlib.Path("index.go"); s = p.read_text()
s = s.replace("func (ix *Index) Add(key string, line int) {\n",
              "func (ix *Index) Add(key string, line int) {\n\tif ix.byKey == nil {\n\t\tix.byKey = make(map[string][]int)\n\t}\n")
p.write_text(s)
P
