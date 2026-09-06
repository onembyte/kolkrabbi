package provider

import "context"

type effortKey struct{}

// WithEffort carries the turn's effort rung to whatever backend serves it. A
// keyed vendor client projects it onto the vendor's own reasoning word; the
// gateway and compatible endpoints ignore it, since no one vocabulary fits
// them (plan 03 §reasoning) and a wrong word is a 400.
func WithEffort(ctx context.Context, effort string) context.Context {
	if effort == "" {
		return ctx
	}
	return context.WithValue(ctx, effortKey{}, effort)
}

// EffortFrom is the rung WithEffort stored, or empty.
func EffortFrom(ctx context.Context) string {
	effort, _ := ctx.Value(effortKey{}).(string)
	return effort
}

// reasoningWord projects kolk's rung onto this client's vendor vocabulary,
// empty when the vendor has none on record or the rung is unknown to it.
func (c *Client) reasoningWord(effort string) string {
	if effort == "" {
		return ""
	}
	d, ok := dispositionFor(c.Origin)
	if !ok {
		return ""
	}
	return d.Capabilities.ReasoningByRung[effort]
}
