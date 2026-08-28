package config

// RoutingSettings holds decisions about what a run does when the model it was
// given can no longer answer.
type RoutingSettings struct {
	// OnSubscriptionLimit is ask (the default), switch or stop, for the moment
	// a subscription runs out of allowance mid-run. The engine owns the
	// vocabulary and validates it; this file only remembers the answer, so a
	// config written by a newer kolk still loads in an older one.
	OnSubscriptionLimit string `json:"on_subscription_limit,omitempty"`
	// OnFreeExhausted is free (the default), paid or stop, for the moment no
	// free model can serve — at startup because the catalogue lists none, or
	// mid-run because every one it can reach is rate-limited. Same rule as its
	// neighbour above: the default never bills.
	OnFreeExhausted string `json:"on_free_exhausted,omitempty"`
}
