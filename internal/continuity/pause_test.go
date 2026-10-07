package continuity

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

func TestPauseNoticeDistinguishesResetFromReadiness(t *testing.T) {
	p := Pause{Kind: string(provider.LimitEndpointCapacity), Model: "test/model", ResetAt: time.Now().Add(time.Hour)}
	if got := p.Notice(); !strings.Contains(got, "reset at "+p.Resumes()) || strings.Contains(got, "resumes") {
		t.Fatalf("future reset promised automatic delivery: %q", got)
	}
	p.ResetAt = time.Now().Add(-time.Hour)
	if got := p.Notice(); !strings.Contains(got, "ready to retry") || strings.Contains(got, p.Resumes()) {
		t.Fatalf("expired reset still looks like a future wait: %q", got)
	}
}

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

// A reset time kolk assumed (the vendor gave none) is kolk's own guess: it
// says when kolk will try again, not when the vendor resets. Ollama Cloud's
// usage limit gives no time and resets on its own schedule (5 h / 7 d), so
// "reset at" in fifteen minutes stated something nobody said.
func TestAnAssumedResetIsNotPresentedAsTheVendors(t *testing.T) {
	now := time.Now()
	assumed := PauseFor(provider.Limit{Kind: provider.LimitSubscriptionAllowance, Model: "gpt-oss:120b-cloud"}, "go on", now)
	if got := assumed.RetryStatus(); !strings.HasPrefix(got, "retry at ") {
		t.Fatalf("an assumed reset reads %q, want \"retry at …\"", got)
	}
	for name, limit := range map[string]provider.Limit{
		"reset given":       {Kind: provider.LimitSubscriptionAllowance, ResetAt: now.Add(time.Hour)},
		"retry-after given": {Kind: provider.LimitSubscriptionAllowance, RetryAfter: time.Hour},
	} {
		if got := PauseFor(limit, "", now).RetryStatus(); !strings.HasPrefix(got, "reset at ") {
			t.Errorf("%s reads %q, want \"reset at …\"", name, got)
		}
	}
	// The difference survives a save, and a pause saved before it existed
	// reads as it always did.
	data, _ := json.Marshal(assumed)
	var loaded Pause
	if err := json.Unmarshal(data, &loaded); err != nil || !strings.HasPrefix(loaded.RetryStatus(), "retry at ") {
		t.Fatalf("reloaded = %+v, %v", loaded, err)
	}
	var old Pause
	if err := json.Unmarshal([]byte(`{"kind":"subscription_allowance","reset_at":"`+now.Add(time.Hour).Format(time.RFC3339)+`"}`), &old); err != nil || !strings.HasPrefix(old.RetryStatus(), "reset at ") {
		t.Fatalf("an older saved pause reads %q, %v", old.RetryStatus(), err)
	}
}
