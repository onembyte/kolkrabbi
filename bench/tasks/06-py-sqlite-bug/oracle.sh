#!/usr/bin/env bash
set -euo pipefail
sed -i.bak "s/        JOIN orders/        LEFT JOIN orders/" store.py && rm -f store.py.bak
