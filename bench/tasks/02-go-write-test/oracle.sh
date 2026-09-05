#!/usr/bin/env bash
set -euo pipefail
cat > palindrome_test.go <<'T'
package bench

import "testing"

func TestIsPalindrome(t *testing.T) {
	for _, c := range []struct {
		in   string
		want bool
	}{{"", true}, {"a", true}, {"abba", true}, {"aba", true}, {"abc", false}} {
		if got := IsPalindrome(c.in); got != c.want {
			t.Errorf("IsPalindrome(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
T
