package provider

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func httpErr(status int, body string, header http.Header) error {
	if header == nil {
		header = http.Header{}
	}
	return newHTTPError(status, header, []byte(body))
}

func TestAdviseCoversTheStatusesUsersActuallyHit(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantIn     string
		wantAction string
	}{
		{
			name:       "401 is a key problem, not a model problem",
			err:        httpErr(http.StatusUnauthorized, `{"error":{"message":"No auth credentials found"}}`, nil),
			wantIn:     "key",
			wantAction: "/key",
		},
		{
			name:       "402 means the account cannot pay for this model",
			err:        httpErr(http.StatusPaymentRequired, `{"error":{"message":"Insufficient credits"}}`, nil),
			wantIn:     "credit",
			wantAction: "/models",
		},
		{
			name:       "404 is a model id that does not exist",
			err:        httpErr(http.StatusNotFound, `{"error":{"message":"No endpoints found"}}`, nil),
			wantIn:     "model",
			wantAction: "/models",
		},
		{
			name:       "408 is a timeout worth retrying",
			err:        httpErr(http.StatusRequestTimeout, "", nil),
			wantIn:     "timed out",
			wantAction: "again",
		},
		{
			name:       "429 is rate limiting",
			err:        httpErr(http.StatusTooManyRequests, `{"error":{"message":"rate limit"}}`, nil),
			wantIn:     "rate",
			wantAction: "free",
		},
		{
			name:       "503 is the provider's problem, not the user's",
			err:        httpErr(http.StatusServiceUnavailable, "", nil),
			wantIn:     "OpenRouter",
			wantAction: "again",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			advice, ok := Advise(tc.err)
			if !ok {
				t.Fatalf("Advise returned no advice for %v", tc.err)
			}
			if !strings.Contains(strings.ToLower(advice.Summary), strings.ToLower(tc.wantIn)) {
				t.Errorf("summary %q does not mention %q", advice.Summary, tc.wantIn)
			}
			if advice.NextAction == "" {
				t.Fatalf("advice for %v has no next action", tc.err)
			}
			if !strings.Contains(advice.NextAction, tc.wantAction) {
				t.Errorf("next action %q does not mention %q", advice.NextAction, tc.wantAction)
			}
		})
	}
}

// Every advice line is read by a person in a terminal at the moment something
// broke. A summary that ends in a full stop and a next action that names no
// command are both ways of saying nothing.
func TestAdviceIsShapedForAPersonInAHurry(t *testing.T) {
	for _, status := range []int{401, 402, 403, 404, 408, 429, 500, 502, 503, 504} {
		advice, ok := Advise(httpErr(status, "", nil))
		if !ok {
			t.Fatalf("HTTP %d has no advice", status)
		}
		if strings.TrimSpace(advice.Summary) != advice.Summary || advice.Summary == "" {
			t.Errorf("HTTP %d: summary %q is empty or badly trimmed", status, advice.Summary)
		}
		if strings.HasSuffix(advice.Summary, ".") {
			t.Errorf("HTTP %d: summary %q ends in a full stop; these are lines, not prose", status, advice.Summary)
		}
		if len(advice.Summary) > 90 {
			t.Errorf("HTTP %d: summary is %d characters, too long for one terminal line", status, len(advice.Summary))
		}
		if strings.TrimSpace(advice.NextAction) == "" {
			t.Errorf("HTTP %d: no next action", status)
		}
	}
}

func TestAdviseReportsRetryAfterWhenTheProviderSaysWhen(t *testing.T) {
	header := http.Header{}
	header.Set("Retry-After", "42")
	advice, ok := Advise(httpErr(http.StatusTooManyRequests, "", header))
	if !ok {
		t.Fatal("no advice for a 429 with Retry-After")
	}
	if advice.RetryAfter != 42*time.Second {
		t.Errorf("RetryAfter = %v, want 42s", advice.RetryAfter)
	}
	if !strings.Contains(advice.NextAction, "42") {
		t.Errorf("next action %q does not tell the user how long to wait", advice.NextAction)
	}
}

// A context overflow arrives as a 400, and a generic "the request was rejected"
// line would be the wrong thing to say about it.
func TestAdviseSeparatesContextOverflowFromAPlainBadRequest(t *testing.T) {
	overflow, ok := Advise(httpErr(http.StatusBadRequest,
		`{"error":{"message":"This endpoint's maximum context length is 8192 tokens"}}`, nil))
	if !ok {
		t.Fatal("no advice for a context overflow")
	}
	if !strings.Contains(strings.ToLower(overflow.Summary), "context") {
		t.Errorf("overflow summary %q does not mention the context window", overflow.Summary)
	}

	plain, ok := Advise(httpErr(http.StatusBadRequest, `{"error":{"message":"bad parameter"}}`, nil))
	if !ok {
		t.Fatal("no advice for a plain 400")
	}
	if plain.Summary == overflow.Summary {
		t.Error("a plain 400 and a context overflow give the same advice")
	}
}

// The model that cannot call tools is the one failure a person is most likely
// to blame on kolk, because the model answers normally right up until it has to
// use a tool.
func TestAdviseRecognisesAModelThatCannotCallTools(t *testing.T) {
	advice, ok := Advise(httpErr(http.StatusBadRequest,
		`{"error":{"message":"No endpoints found that support tool use"}}`, nil))
	if !ok {
		t.Fatal("no advice for a model without tool support")
	}
	if !strings.Contains(strings.ToLower(advice.Summary), "tool") {
		t.Errorf("summary %q does not say the model cannot use tools", advice.Summary)
	}
	if !strings.Contains(advice.NextAction, "/models") {
		t.Errorf("next action %q does not point at a model that can", advice.NextAction)
	}
}

// A connection that dies mid-stream is not an HTTPError at all: nothing was
// refused, the bytes simply stopped.
func TestAdviseCoversATransportFailure(t *testing.T) {
	for _, err := range []error{
		fmt.Errorf("reading the stream: %w", errors.New("unexpected EOF")),
		fmt.Errorf("post: %w", &net.OpError{Op: "dial", Err: errors.New("connection refused")}),
	} {
		advice, ok := Advise(err)
		if !ok {
			t.Fatalf("no advice for transport failure %v", err)
		}
		if advice.NextAction == "" {
			t.Errorf("transport failure %v has no next action", err)
		}
	}
}

func TestAdviseStaysQuietWhenItHasNothingToAdd(t *testing.T) {
	for _, err := range []error{nil, errors.New("something else entirely"), context.Canceled} {
		if _, ok := Advise(err); ok {
			t.Errorf("Advise volunteered advice for %v", err)
		}
	}
}
