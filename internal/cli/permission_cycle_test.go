package cli

import (
	"testing"

	"github.com/onembyte/kolkrabbi/internal/engine"
)

// Shift+Tab walks the same three tiers /permissions lists, in the same order,
// and wraps — so the key can always undo itself without reaching for a command.
func TestNextPermissionWalksEveryTierAndWraps(t *testing.T) {
	want := []engine.Permission{
		engine.PermissionAutoApprove,
		engine.PermissionFullAuto,
		engine.PermissionAsk,
	}
	current := engine.PermissionAsk
	for step, expected := range want {
		current = nextPermission(current)
		if current != expected {
			t.Fatalf("step %d = %q, want %q", step, current, expected)
		}
	}
}

// An unset tier is the default tier. The first press must move somewhere
// predictable rather than somewhere arbitrary.
func TestNextPermissionFromAnUnsetTierIsPredictable(t *testing.T) {
	for _, unset := range []engine.Permission{"", "yolo", "AUTO-APPROVE"} {
		if got := nextPermission(unset); got != engine.PermissionAutoApprove {
			t.Fatalf("next after %q = %q, want %q", unset, got, engine.PermissionAutoApprove)
		}
	}
}

// The cycle may only ever produce a tier the engine recognises: an unnormalised
// value would silently weaken or strengthen what the session refuses.
func TestEveryCycledTierIsOneTheEngineKnows(t *testing.T) {
	current := engine.PermissionAsk
	for range permissionCycle {
		current = nextPermission(current)
		if _, ok := engine.NormalizePermission(string(current)); !ok {
			t.Fatalf("cycle produced a tier the engine rejects: %q", current)
		}
	}
}
