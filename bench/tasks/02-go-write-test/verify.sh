#!/usr/bin/env bash
set -uo pipefail
ls palindrome_test.go >/dev/null 2>&1 || { echo "no palindrome_test.go was written"; exit 1; }
go test ./... >/dev/null 2>&1 || { echo "go test fails on the unmodified function"; exit 1; }
cp palindrome.go .palindrome.orig
# Mutation: make it always report true. A meaningful test must now fail.
cat > palindrome.go <<'MUT'
package bench

func IsPalindrome(s string) bool { return true }
MUT
if go test ./... >/dev/null 2>&1; then
  cp .palindrome.orig palindrome.go; rm -f .palindrome.orig
  echo "the test passes even when IsPalindrome always returns true, so it tests nothing"
  exit 1
fi
cp .palindrome.orig palindrome.go; rm -f .palindrome.orig
echo "test present and it detects a broken implementation"
