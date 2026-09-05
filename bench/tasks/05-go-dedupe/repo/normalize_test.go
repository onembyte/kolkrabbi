package bench

import "testing"

func TestNormalize(t *testing.T) {
	if got := normalizeUser("  Ada  "); got != "ada" {
		t.Errorf("normalizeUser = %q", got)
	}
	if got := normalizeTeam("  Core Team "); got != "core team" {
		t.Errorf("normalizeTeam = %q", got)
	}
}
