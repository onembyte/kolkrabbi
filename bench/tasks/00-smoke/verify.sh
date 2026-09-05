#!/usr/bin/env bash
# Passes when the harness created the file the mock is scripted to create.
set -uo pipefail
[ -f hello-from-mock.txt ] || { echo "hello-from-mock.txt was not created"; exit 1; }
grep -q "smoke test" hello-from-mock.txt || { echo "file exists but content is unexpected"; exit 1; }
echo "hello-from-mock.txt present with expected content"
