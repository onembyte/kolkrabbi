package provider

// Billing modes, plan 24's honest vocabulary: what a reply cost is either the
// gateway's own figure, a subscription's turn, a vendor's metered tokens that
// the reply did not price, a local model's nothing, or unknown. Never an
// estimate dressed as the provider's figure.
const (
	BillingGateway      = "gateway"
	BillingSubscription = "subscription"
	BillingAPIMetered   = "api-metered"
	BillingLocal        = "local"
	BillingUnknown      = "unknown"
)

// billing is the mode this client's origin implies.
func (c *Client) billing() string {
	switch {
	case c.Origin == "":
		return BillingGateway
	case c.Origin == HostOrigin:
		return BillingLocal
	case c.auth != nil:
		return BillingAPIMetered
	}
	return BillingUnknown
}
