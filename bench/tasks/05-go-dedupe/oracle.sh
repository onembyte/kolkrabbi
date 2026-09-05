#!/usr/bin/env bash
set -euo pipefail
cat > normalize.go <<'N'
package bench

import "strings"

func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func normalizeUser(name string) string { return normalizeName(name) }

func normalizeTeam(name string) string { return normalizeName(name) }
N
