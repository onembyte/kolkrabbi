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
	Kind      string    `json:"kind"`
	Scope     string    `json:"scope"`
	Connector string    `json:"connector,omitempty"`
	Model     string    `json:"model,omitempty"`
	Message   string    `json:"message,omitempty"`
	Since     time.Time `json:"since"`
	ResetAt   time.Time `json:"reset_at"`
	// Estimated marks a ResetAt kolk assumed because the vendor gave none:
	// when kolk will try again, not when the vendor resets.
	Estimated   bool   `json:"estimated,omitempty"`
	PendingTurn string `json:"pending_turn,omitempty"`
}

// Resumes formats the recorded reset time in the reader's clock.
func (p Pause) Resumes() string { return ResetClock(p.ResetAt, time.Now(), time.Local) }

// ResetClock is when a limit lifts, as a person reading at now in loc says it.
// A vendor's reset can be days away, so a time that is not today names its
// day: the weekday within the coming week, the date beyond it, where a weekday
// alone would read as this one. Every display of a reset goes through here.
func ResetClock(reset, now time.Time, loc *time.Location) string {
	reset, now = reset.In(loc), now.In(loc)
	// Calendar dates compared as UTC midnights: whole days, with no daylight
	// saving hour in between.
	day := func(t time.Time) time.Time {
		y, m, d := t.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	switch days := int(day(reset).Sub(day(now)) / (24 * time.Hour)); {
	case days <= 0:
		return reset.Format("15:04")
	case days < 7:
		return reset.Format("Mon 15:04")
	}
	return reset.Format("Jan 2 15:04")
}

// RetryStatus describes the recorded reset without promising that a closed or
// manually paused session will run by itself.
func (p Pause) RetryStatus() string {
	if !p.ResetAt.After(time.Now()) {
		return "ready to retry"
	}
	if p.Estimated {
		return "retry at " + p.Resumes()
	}
	return "reset at " + p.Resumes()
}

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
//
// A reset counts only while it is still ahead. One already past, a stale window
// or a skewed clock, would lift the pause at once into the same limit and the
// same stale time, again and again; kolk estimates instead.
func PauseFor(limit provider.Limit, pending string, now time.Time) Pause {
	until, estimated := limit.ResetAt, false
	switch {
	case until.After(now):
	case limit.RetryAfter > 0:
		until = now.Add(limit.RetryAfter)
	default:
		until, estimated = now.Add(limit.Kind.DefaultCooldown()), true
	}
	return Pause{
		Kind: string(limit.Kind), Scope: string(limit.Scope), Connector: limit.Connector, Model: limit.Model,
		Message: secret.Scrub(limit.Message), Since: now, ResetAt: until, Estimated: estimated, PendingTurn: pending,
	}
}

// Limit is the limit this pause was made from, for the event that says it is
// over: same kind, scope, model and connector; the timing is the pause's own.
// A reset kolk only estimated is left out: published, it would read as the
// vendor's, and the protocol leaves reset_at absent when it is unknown.
func (p Pause) Limit() provider.Limit {
	limit := provider.Limit{
		Kind: provider.LimitKind(p.Kind), Scope: provider.LimitScope(p.Scope),
		Model: p.Model, Connector: p.Connector, Message: p.Message,
	}
	if !p.Estimated {
		limit.ResetAt = p.ResetAt
	}
	return limit
}

// Notice is the one line a surface shows for a pause: what is paused, why, and
// when it can retry. The same words on the status line, in /doctor and in the
// pause message, so a reader meets one vocabulary.
func (p Pause) Notice() string {
	what := p.Model
	if what == "" {
		what = p.Connector
	}
	if what == "" {
		return "paused · " + p.HumanKind() + " · " + p.RetryStatus()
	}
	return "paused · " + what + " · " + p.HumanKind() + " · " + p.RetryStatus()
}
