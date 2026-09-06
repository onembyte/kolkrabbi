// Package continuity holds the state and rules of plan 35 that more than one
// layer must agree on: what a paused session is, and which limits waiting can
// lift. The engine decides when to pause; the session stores it; the surfaces
// show it. None of them may define it alone.
package continuity

import (
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/secret"
)

// Pause is a session stopped by a limit that will lift (plan 35 §2.2): what
// stopped it, when it lifts, and the turn that was asked for, kept verbatim so
// nothing is lost and nothing is re-typed. Persisted with the session.
type Pause struct {
	Kind        string    `json:"kind"`
	Scope       string    `json:"scope"`
	Connector   string    `json:"connector,omitempty"`
	Model       string    `json:"model,omitempty"`
	Message     string    `json:"message,omitempty"`
	Since       time.Time `json:"since"`
	ResetAt     time.Time `json:"reset_at"`
	PendingTurn string    `json:"pending_turn,omitempty"`
}

// Resumes says when, in the reader's clock.
func (p Pause) Resumes() string { return p.ResetAt.Local().Format("15:04") }

// HumanKind names the limit the way a person says it.
func (p Pause) HumanKind() string { return HumanKind(provider.LimitKind(p.Kind)) }

// HumanKind names a limit kind the way a person says it.
func HumanKind(kind provider.LimitKind) string {
	switch kind {
	case provider.LimitSubscriptionAllowance:
		return "subscription allowance"
	case provider.LimitAccountQuota:
		return "account quota"
	case provider.LimitEndpointCapacity:
		return "capacity limit"
	case provider.LimitTransport:
		return "endpoint (unreachable)"
	case provider.LimitModelRefusal:
		return "model refusal"
	case provider.LimitBudgetStop:
		return "budget stop"
	}
	return string(kind)
}

// Pausable says which limits waiting can lift. A model refusing this request
// and kolk's own budget stop do not lift by themselves; those stop.
func Pausable(limit provider.Limit) bool {
	switch limit.Kind {
	case provider.LimitSubscriptionAllowance, provider.LimitAccountQuota, provider.LimitEndpointCapacity, provider.LimitTransport:
		return true
	}
	return false
}

// PauseFor builds the pause for a limit met now: the vendor's reset, its
// Retry-After, or the kind's default -- the same rule the cooldown uses.
func PauseFor(limit provider.Limit, pending string, now time.Time) Pause {
	until := limit.ResetAt
	switch {
	case !until.IsZero():
	case limit.RetryAfter > 0:
		until = now.Add(limit.RetryAfter)
	default:
		until = now.Add(limit.Kind.DefaultCooldown())
	}
	return Pause{
		Kind: string(limit.Kind), Scope: string(limit.Scope), Connector: limit.Connector, Model: limit.Model,
		Message: secret.Scrub(limit.Message), Since: now, ResetAt: until, PendingTurn: pending,
	}
}
