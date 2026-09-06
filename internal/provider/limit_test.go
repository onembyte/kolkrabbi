package provider

import (
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"
)

// Every limit kolk can hit, told apart. Two 429s are not the same event: one
// is the plan's window, the other the endpoint's capacity, and the right next
// move differs -- so does the cooldown, and so does what the user is told.
func TestClassifyTellsTheLimitsApart(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		kind  LimitKind
		scope LimitScope
		reset time.Duration
	}{
		{"plan allowance", &HTTPError{StatusCode: http.StatusTooManyRequests, LimitSource: "subscription plan", RetryAfter: 40 * time.Minute}, LimitSubscriptionAllowance, ScopeAccount, 40 * time.Minute},
		{"quota word", &HTTPError{StatusCode: http.StatusTooManyRequests, LimitSource: "quota"}, LimitSubscriptionAllowance, ScopeAccount, 0},
		{"credits", &HTTPError{StatusCode: http.StatusPaymentRequired}, LimitAccountQuota, ScopeAccount, 0},
		{"bare 429 on the keyed origin", &HTTPError{StatusCode: http.StatusTooManyRequests, RetryAfter: 3 * time.Second}, LimitEndpointCapacity, ScopeKey, 3 * time.Second},
		{"bare 429 on a compatible endpoint", &HTTPError{StatusCode: http.StatusTooManyRequests, Origin: CompatibleOrigin}, LimitEndpointCapacity, ScopeEndpoint, 0},
		{"overloaded 503", &HTTPError{StatusCode: http.StatusServiceUnavailable}, LimitEndpointCapacity, ScopeEndpoint, 0},
		{"overloaded 529", &HTTPError{StatusCode: 529}, LimitEndpointCapacity, ScopeEndpoint, 0},
		{"context length", &HTTPError{StatusCode: http.StatusBadRequest, Message: "This model's maximum context length is 128000 tokens"}, LimitModelRefusal, ScopeModel, 0},
		{"model gone", &HTTPError{StatusCode: http.StatusNotFound, Message: "model not found"}, LimitModelRefusal, ScopeModel, 0},
		{"cli usage limit phrase", errors.New("claude: usage limit reached; resets at 4pm"), LimitSubscriptionAllowance, ScopeAccount, 0},
		{"transport", &url.Error{Op: "Post", URL: "http://host.invalid/v1", Err: errors.New("dial tcp: connection refused")}, LimitTransport, ScopeEndpoint, 0},
		{"already classified", Limit{Kind: LimitBudgetStop, Scope: ScopeAccount, Message: "run reached $1.00"}, LimitBudgetStop, ScopeAccount, 0},
	} {
		limit, ok := Classify(tc.err)
		if !ok {
			t.Fatalf("%s: not classified", tc.name)
		}
		if limit.Kind != tc.kind || limit.Scope != tc.scope || limit.RetryAfter != tc.reset {
			t.Fatalf("%s: got %+v, want kind %s scope %s retry %s", tc.name, limit, tc.kind, tc.scope, tc.reset)
		}
		if limit.Source == "" {
			t.Fatalf("%s: no source recorded", tc.name)
		}
	}
}

// Not everything that fails is a limit. A 401, a malformed request, a nil: no
// classification, so no cooldown is ever written for them.
func TestClassifyRefusesWhatIsNotALimit(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"unauthorised", &HTTPError{StatusCode: http.StatusUnauthorized}},
		{"bad request without a limit phrase", &HTTPError{StatusCode: http.StatusBadRequest, Message: "messages[0].role is invalid"}},
		{"plain error", errors.New("connection reset by peer")},
		{"nil", nil},
	} {
		if limit, ok := Classify(tc.err); ok {
			t.Fatalf("%s classified as %+v", tc.name, limit)
		}
	}
}

// The limit's message is scrubbed like every other error text, and the kind's
// default cooldown is what the registry uses when the vendor gave no reset.
func TestLimitDefaultsAreStatedPerKind(t *testing.T) {
	for kind, want := range map[LimitKind]time.Duration{
		LimitEndpointCapacity:      60 * time.Second,
		LimitSubscriptionAllowance: 15 * time.Minute,
		LimitAccountQuota:          24 * time.Hour,
		LimitModelRefusal:          time.Hour,
		LimitTransport:             30 * time.Second,
		LimitBudgetStop:            0,
	} {
		if got := kind.DefaultCooldown(); got != want {
			t.Fatalf("%s default cooldown = %s, want %s", kind, got, want)
		}
	}
}
