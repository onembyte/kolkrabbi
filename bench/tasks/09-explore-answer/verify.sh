#!/usr/bin/env bash
set -uo pipefail
[ -f ANSWER.txt ] || { echo "ANSWER.txt was not written"; exit 1; }
ans=$(tr -d " \t\r\n\"'\`" < ANSWER.txt)
[ "$ans" = "checkExpiry" ] || { echo "ANSWER.txt says '$ans', expected checkExpiry"; exit 1; }
echo "correct function identified"
