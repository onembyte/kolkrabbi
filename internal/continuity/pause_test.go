package continuity

import (
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// Waiting lifts a plan's window, an account's credit, an endpoint's capacity
// and a dead connection. It does not lift a model's refusal of this request
// or kolk's own budget stop; those are stops.
func TestOnlyLimitsThatWaitingLiftsArePausable(t *testing.T) {
	for kind, want := range map[provider.LimitKind]bool{
		provider.LimitSubscriptionAllowance: true, provider.LimitAccountQuota: true, provider.LimitEndpointCapacity: true,
		provider.LimitTransport: true, provider.LimitModelRefusal: false, provider.LimitBudgetStop: false,
	} {
		if got := Pausable(provider.Limit{Kind: kind}); got != want {
			t.Fatalf("Pausable(%s) = %v, want %v", kind, got, want)
		}
	}
}

// The reset comes from the vendor when it gave one, its Retry-After when it
// gave that, and the kind's default otherwise -- the cooldown's own rule.
func TestPauseForPicksTheResetTheWayTheCooldownDoes(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	reset := now.Add(3 * time.Hour)
	if p := PauseFor(provider.Limit{Kind: provider.LimitSubscriptionAllowance, ResetAt: reset, RetryAfter: time.Minute}, "x", now); !p.ResetAt.Equal(reset) {
		t.Fatalf("vendor reset ignored: %s", p.ResetAt)
	}
	if p := PauseFor(provider.Limit{Kind: provider.LimitEndpointCapacity, RetryAfter: 90 * time.Second}, "x", now); !p.ResetAt.Equal(now.Add(90 * time.Second)) {
		t.Fatalf("retry-after ignored: %s", p.ResetAt)
	}
	p := PauseFor(provider.Limit{Kind: provider.LimitTransport, Message: "dial tcp: refused"}, "the turn", now)
	if !p.ResetAt.Equal(now.Add(30*time.Second)) || p.PendingTurn != "the turn" || p.Since != now {
		t.Fatalf("default pause = %+v", p)
	}
}
