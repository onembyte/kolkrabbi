package engine

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

func cooldownsAt(t *testing.T, sessionFile, sharedFile string, now *time.Time) *Cooldowns {
	t.Helper()
	return openCooldownsAt(sessionFile, sharedFile, func() time.Time { return *now })
}

// A limit is remembered past the retry loop that met it. An account-scope
// limit -- the plan's window -- is written where every session of this user
// reads it; a model-scope one stays with the session that hit it.
func TestALimitIsRememberedAcrossSessionsByItsScope(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	shared := filepath.Join(dir, "cooldowns.json")
	first := cooldownsAt(t, filepath.Join(dir, "s1.cooldowns.json"), shared, &now)

	plan := provider.Limit{Kind: provider.LimitSubscriptionAllowance, Scope: provider.ScopeAccount, Connector: "claude", Model: "claude-fable", ResetAt: now.Add(40 * time.Minute), Source: "vendor-frame"}
	if _, ok := first.Mark(plan); !ok {
		t.Fatal("an allowance limit was not recorded")
	}
	perModel := provider.Limit{Kind: provider.LimitModelRefusal, Scope: provider.ScopeModel, Connector: "openrouter", Model: "vendor/model", Source: "phrase"}
	if _, ok := first.Mark(perModel); !ok {
		t.Fatal("a model refusal was not recorded")
	}

	second := cooldownsAt(t, filepath.Join(dir, "s2.cooldowns.json"), shared, &now)
	if cd, ok := second.Cooling(provider.ScopeAccount, "claude", ""); !ok || !cd.Until.Equal(now.Add(40*time.Minute)) {
		t.Fatalf("another session does not see the plan's cooldown: %+v %v", cd, ok)
	}
	if _, ok := second.Cooling(provider.ScopeModel, "openrouter", "vendor/model"); ok {
		t.Fatal("a model-scope cooldown leaked out of its session")
	}
	if _, ok := first.Cooling(provider.ScopeModel, "openrouter", "vendor/model"); !ok {
		t.Fatal("the session that hit the refusal forgot it")
	}
}

// Without a vendor reset, the kind's default holds; then it lifts on its own.
// A budget stop is the user's to lift and is never a cooldown.
func TestDefaultsAndExpiry(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	c := cooldownsAt(t, filepath.Join(dir, "s.cooldowns.json"), filepath.Join(dir, "shared.json"), &now)

	cd, ok := c.Mark(provider.Limit{Kind: provider.LimitEndpointCapacity, Scope: provider.ScopeEndpoint, Connector: "openrouter", Source: "status"})
	if !ok || !cd.Until.Equal(now.Add(60*time.Second)) {
		t.Fatalf("capacity default = %+v %v, want 60s from now", cd, ok)
	}
	cd, ok = c.Mark(provider.Limit{Kind: provider.LimitEndpointCapacity, Scope: provider.ScopeEndpoint, Connector: "other", RetryAfter: 5 * time.Minute, Source: "retry-after"})
	if !ok || !cd.Until.Equal(now.Add(5*time.Minute)) {
		t.Fatalf("retry-after not honoured: %+v", cd)
	}
	if _, ok := c.Mark(provider.Limit{Kind: provider.LimitBudgetStop, Scope: provider.ScopeAccount, Source: "kolk"}); ok {
		t.Fatal("a budget stop became a cooldown")
	}
	if _, ok := c.Cooling(provider.ScopeEndpoint, "openrouter", ""); !ok {
		t.Fatal("not cooling right after the mark")
	}
	now = now.Add(61 * time.Second)
	if _, ok := c.Cooling(provider.ScopeEndpoint, "openrouter", ""); ok {
		t.Fatal("still cooling after the default lifted")
	}
	if _, ok := c.Cooling(provider.ScopeEndpoint, "other", ""); !ok {
		t.Fatal("the longer cooldown lifted early")
	}
}

// A running session learns of a limit another session just hit, without a
// restart: the shared file is re-read on demand.
func TestReloadSeesAnotherSessionsMark(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	shared := filepath.Join(dir, "cooldowns.json")
	a := cooldownsAt(t, filepath.Join(dir, "a.cooldowns.json"), shared, &now)
	b := cooldownsAt(t, filepath.Join(dir, "b.cooldowns.json"), shared, &now)
	if _, ok := b.Cooling(provider.ScopeAccount, "codex", ""); ok {
		t.Fatal("cooling before anything happened")
	}
	if _, ok := a.Mark(provider.Limit{Kind: provider.LimitSubscriptionAllowance, Scope: provider.ScopeAccount, Connector: "codex", ResetAt: now.Add(time.Hour), Source: "limit_source"}); !ok {
		t.Fatal("mark refused")
	}
	if err := b.Reload(); err != nil {
		t.Fatal(err)
	}
	if _, ok := b.Cooling(provider.ScopeAccount, "codex", ""); !ok {
		t.Fatal("the other session's plan limit is not seen after reload")
	}
}
