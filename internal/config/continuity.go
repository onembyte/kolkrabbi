package config

import "strings"

// ContinuitySettings is what a session does when the model behind it hits a
// limit (plan 35 §2.4). Resume is auto (default) or manual. Mode is off
// (default: pause, resume on the same model) or on (walk the chain). Select
// is auto (the equivalents, in Order), preferred (Preferred, as written) or
// ask (a question, once per run). Order is the three billing groups in the
// order to try them; Preferred is the person's own list of models, plan-
// qualified or bare. Every unset key inherits its default; the two old
// routing knobs are read as aliases until the release after this one.
type ContinuitySettings struct {
	Resume    string   `json:"resume,omitempty"`
	Mode      string   `json:"mode,omitempty"`
	Select    string   `json:"select,omitempty"`
	Preferred []string `json:"preferred,omitempty"`
	Order     []string `json:"order,omitempty"`
}

// DefaultContinuityOrder is the owner's: subscriptions, then paid, then free.
var DefaultContinuityOrder = []string{"subscription", "paid", "free"}

// EffectiveContinuity is the block with every default filled and the old
// routing knobs folded in as aliases (plan 35 §2.6): `switch` is mode on with
// select auto, `stop` is mode off, on_free_exhausted `paid` is paid before
// free. An explicit continuity key always wins over an alias.
func (c *Config) EffectiveContinuity() ContinuitySettings {
	out := c.Continuity
	if out.Resume == "" {
		out.Resume = "auto"
	}
	if out.Mode == "" {
		switch strings.ToLower(strings.TrimSpace(c.Routing.OnSubscriptionLimit)) {
		case "switch":
			out.Mode = "on"
		default:
			out.Mode = "off"
		}
	}
	if out.Select == "" {
		out.Select = "auto"
	}
	if len(out.Order) == 0 {
		out.Order = append([]string(nil), DefaultContinuityOrder...)
	}
	out.Order = NormalizeContinuityOrderWords(out.Order)
	return out
}

// NormalizeContinuityOrderWords spells the groups one way: subs and
// subscriptions are subscription, metered and keys are paid.
func NormalizeContinuityOrderWords(order []string) []string {
	out := make([]string, 0, len(order))
	for _, word := range order {
		switch strings.ToLower(strings.TrimSpace(word)) {
		case "subs", "subscriptions", "subscription":
			out = append(out, "subscription")
		case "paid", "metered", "keys", "key":
			out = append(out, "paid")
		case "free":
			out = append(out, "free")
		default:
			out = append(out, strings.ToLower(strings.TrimSpace(word)))
		}
	}
	return out
}
