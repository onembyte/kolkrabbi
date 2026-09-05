#!/usr/bin/env bash
set -euo pipefail
python3 - <<'P'
import re, pathlib
p = pathlib.Path("config.go"); s = p.read_text()
s = s.replace("\tn, _ := strconv.Atoi(s)\n\treturn n, nil",
              "\tn, err := strconv.Atoi(s)\n\tif err != nil {\n\t\treturn 0, err\n\t}\n\treturn n, nil")
p.write_text(s)
P
