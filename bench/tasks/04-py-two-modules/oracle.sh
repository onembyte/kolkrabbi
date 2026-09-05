#!/usr/bin/env bash
set -euo pipefail
cat >> money.py <<'M'


def cents_to_str(cents):
    """Format an integer number of cents as a currency string."""
    sign = "-" if cents < 0 else ""
    cents = abs(cents)
    return "%s$%d.%02d" % (sign, cents // 100, cents % 100)
M
cat > report.py <<'R'
from money import cents_to_str


def render_line(label, cents):
    """Render one report line."""
    return "%s: %s" % (label, cents_to_str(cents))
R
