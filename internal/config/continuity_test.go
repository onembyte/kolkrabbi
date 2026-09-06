package config

import (
	"strings"
	"testing"
)

// V35.5, plan 35 §2.4 and §2.6: the continuity block names the mode, the
// selection, the person's list and the order; the two old routing knobs keep
// working as aliases for one release — switch means mode on with select auto,
// stop means mode off, on_free_exhausted paid means paid before free — and an
// explicit continuity key always wins over an alias.
func TestContinuityResolvesItsDefaultsAndTheOldAliases(t *testing.T) {
	got := (&Config{}).EffectiveContinuity()
	if got.Mode != "off" || got.Select != "auto" || strings.Join(got.Order, ",") != "subscription,paid,free" || got.Resume != "auto" {
		t.Fatalf("defaults = %+v", got)
	}
	got = (&Config{Routing: RoutingSettings{OnSubscriptionLimit: "switch"}}).EffectiveContinuity()
	if got.Mode != "on" || got.Select != "auto" {
		t.Fatalf("switch alias = %+v, want mode on select auto", got)
	}
	got = (&Config{Routing: RoutingSettings{OnSubscriptionLimit: "stop"}}).EffectiveContinuity()
	if got.Mode != "off" {
		t.Fatalf("stop alias = %+v, want mode off", got)
	}
	got = (&Config{Routing: RoutingSettings{OnFreeExhausted: "paid"}}).EffectiveContinuity()
	if strings.Join(got.Order, ",") != "subscription,paid,free" {
		t.Fatalf("paid alias = %+v, want paid before free", got)
	}
	got = (&Config{Routing: RoutingSettings{OnSubscriptionLimit: "switch"}, Continuity: ContinuitySettings{Mode: "off", Select: "ask", Preferred: []string{"ChatGPT Plus/gpt-5.6-sol"}, Order: []string{"paid", "subscription", "free"}}}).EffectiveContinuity()
	if got.Mode != "off" || got.Select != "ask" || len(got.Preferred) != 1 || strings.Join(got.Order, ",") != "paid,subscription,free" {
		t.Fatalf("explicit keys did not win over the alias: %+v", got)
	}
}
