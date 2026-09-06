package provider

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/secret"
)

// LimitKind is the closed vocabulary of the ways a model stops answering for
// reasons that are not the user's fault (plan 35 §2.0). Every policy in the
// continuity engine reads this and nothing looser.
type LimitKind string

const (
	LimitSubscriptionAllowance LimitKind = "subscription_allowance" // the plan's window: usage limits, credits_required on a plan row
	LimitAccountQuota          LimitKind = "account_quota"          // a metered account out of credit
	LimitEndpointCapacity      LimitKind = "endpoint_capacity"      // a 429 with no allowance source; 5xx overloaded
	LimitBudgetStop            LimitKind = "budget_stop"            // kolk's own ceilings
	LimitModelRefusal          LimitKind = "model_refusal"          // this model will not take this request: context, policy, gone
	LimitTransport             LimitKind = "transport"              // the endpoint could not be reached at all
)

// LimitScope is what a cooldown for the limit is keyed on.
type LimitScope string

const (
	ScopeModel    LimitScope = "model"
	ScopeKey      LimitScope = "key"
	ScopeAccount  LimitScope = "account"
	ScopeEndpoint LimitScope = "endpoint"
)

// Limit is one classified stop: what kind, what it is keyed on, when it lifts
// (zero when unknown), and how kolk knows. It is also an error, so a caller
// that has already classified -- the engine's own budget stops -- can hand it
// through Classify unchanged.
type Limit struct {
	Kind       LimitKind
	Scope      LimitScope
	Model      string
	Connector  string
	ResetAt    time.Time     // zero: unknown
	RetryAfter time.Duration // the provider's own answer to "how long", when given
	Message    string        // scrubbed
	Source     string        // retry-after | limit_source | status | phrase | vendor-frame | kolk
}

func (l Limit) Error() string {
	if l.Message == "" {
		return string(l.Kind)
	}
	return string(l.Kind) + ": " + l.Message
}

// DefaultCooldown is how long a limit of this kind is assumed to hold when the
// vendor gave no reset time. Starting points, logged with their source, and
// never treated as permanent. A budget stop has no cooldown: the user lifts it.
func (k LimitKind) DefaultCooldown() time.Duration {
	switch k {
	case LimitEndpointCapacity:
		return 60 * time.Second
	case LimitSubscriptionAllowance:
		return 15 * time.Minute
	case LimitAccountQuota:
		return 24 * time.Hour
	case LimitModelRefusal:
		return time.Hour
	case LimitTransport:
		return 30 * time.Second
	}
	return 0
}

// Classify reads an error and says which limit it is, if it is one. Pure and
// table-driven: status first, the vendor's own limit_source second, the phrases
// kolk has met third. Anything else is not a limit, and no policy acts on it.
func Classify(err error) (Limit, bool) {
	if err == nil {
		return Limit{}, false
	}
	var already Limit
	if errors.As(err, &already) {
		if already.Source == "" {
			already.Source = "kolk" // the engine's own stops name no vendor
		}
		return already, true
	}
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return classifyHTTP(httpErr)
	}
	var urlErr *url.Error
	var netErr net.Error
	if errors.As(err, &urlErr) || errors.As(err, &netErr) {
		return Limit{Kind: LimitTransport, Scope: ScopeEndpoint, Message: secret.Scrub(err.Error()), Source: "transport"}, true
	}
	if allowancePhrase(err.Error()) {
		return Limit{Kind: LimitSubscriptionAllowance, Scope: ScopeAccount, Message: secret.Scrub(err.Error()), Source: "phrase"}, true
	}
	return Limit{}, false
}

func classifyHTTP(e *HTTPError) (Limit, bool) {
	limit := Limit{Message: secret.Scrub(e.Message), RetryAfter: e.RetryAfter}
	if limit.Message == "" {
		limit.Message = secret.Scrub(e.ResponseBody)
	}
	switch {
	case e.StatusCode == http.StatusPaymentRequired:
		limit.Kind, limit.Scope, limit.Source = LimitAccountQuota, ScopeAccount, "status"
	case e.StatusCode == http.StatusTooManyRequests && limitSourceIsAllowance(e.LimitSource):
		limit.Kind, limit.Scope, limit.Source = LimitSubscriptionAllowance, ScopeAccount, "limit_source"
	case e.StatusCode == http.StatusTooManyRequests && allowancePhrase(limit.Message):
		limit.Kind, limit.Scope, limit.Source = LimitSubscriptionAllowance, ScopeAccount, "phrase"
	case e.StatusCode == http.StatusTooManyRequests:
		// OpenRouter rate-limits per key; a compatible endpoint rate-limits itself.
		// The cooldown is keyed accordingly, so one key's 429 does not cool a
		// whole endpoint and an endpoint's 429 is not blamed on a key.
		limit.Kind, limit.Scope, limit.Source = LimitEndpointCapacity, ScopeKey, "status"
		if e.Origin != "" {
			limit.Scope = ScopeEndpoint
		}
	case e.StatusCode == http.StatusServiceUnavailable, e.StatusCode == 529:
		limit.Kind, limit.Scope, limit.Source = LimitEndpointCapacity, ScopeEndpoint, "status"
	case e.StatusCode == http.StatusNotFound && strings.Contains(strings.ToLower(limit.Message), "model"):
		limit.Kind, limit.Scope, limit.Source = LimitModelRefusal, ScopeModel, "status"
	case e.StatusCode == http.StatusBadRequest && refusalPhrase(limit.Message):
		limit.Kind, limit.Scope, limit.Source = LimitModelRefusal, ScopeModel, "phrase"
	default:
		return Limit{}, false
	}
	if e.RetryAfter > 0 {
		limit.Source = "retry-after; " + limit.Source
	}
	return limit, true
}

// limitSourceIsAllowance reads the vendor's own word for where a 429 came from.
func limitSourceIsAllowance(source string) bool {
	source = strings.ToLower(source)
	for _, word := range []string{"subscription", "plan", "quota", "credit"} {
		if strings.Contains(source, word) {
			return true
		}
	}
	return false
}

// allowancePhrase is the sentence a vendor CLI prints when a plan's window is
// used up, in the forms kolk has met.
func allowancePhrase(message string) bool {
	message = strings.ToLower(message)
	for _, phrase := range []string{"usage limit", "quota exceeded", "out of credit", "plan limit", "subscription limit"} {
		if strings.Contains(message, phrase) {
			return true
		}
	}
	return false
}

// refusalPhrase is a 400 that is about this model and this request, not about
// a malformed call: context length, an unsupported feature, a policy refusal.
func refusalPhrase(message string) bool {
	message = strings.ToLower(message)
	for _, phrase := range []string{"context length", "maximum context", "too many tokens", "does not support", "not supported", "content policy", "unsupported model"} {
		if strings.Contains(message, phrase) {
			return true
		}
	}
	return false
}
