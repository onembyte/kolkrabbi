package bench

import "strings"

func normalizeUser(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func normalizeTeam(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
