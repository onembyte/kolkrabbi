package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/secret"
)

const verifierCanary = "sk-or-v1-0123456789abcdef0123456789abcdef"

func TestOpenRouterKeyVerifierParsesTheCurrentResponse(t *testing.T) {
	var calls int
	rt := verifierTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Method != http.MethodGet || req.URL.String() != DefaultBaseURL+"/key" {
			t.Errorf("request = %s %s", req.Method, req.URL)
		}
		if got := req.Header.Get("Authorization"); got != "Bearer "+verifierCanary {
			t.Errorf("Authorization = %q", got)
		}
		deadline, ok := req.Context().Deadline()
		if !ok {
			t.Error("verification request has no deadline")
		} else if remaining := time.Until(deadline); remaining <= 0 || remaining > 2*time.Second {
			t.Errorf("verification deadline is %v away, want (0, 2s]", remaining)
		}
		return verifierResponse(http.StatusOK, `{"data":{"limit":100,"limit_remaining":74.5,"usage":25.5,"is_free_tier":false}}`), nil
	})

	verifier := OpenRouterVerifier{
		Client: &http.Client{Transport: rt},
	}
	got, err := verifier.Verify(context.Background(), secret.New(verifierCanary))
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("verification made %d requests, want 1", calls)
	}
	if got.LimitUSD == nil || *got.LimitUSD != 100 || got.RemainingUSD == nil || *got.RemainingUSD != 74.5 || got.UsageUSD != 25.5 || got.FreeTier {
		t.Errorf("verification status = %+v", got)
	}
}

func TestOpenRouterKeyVerifierClassifiesAndScrubsFailures(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		transport error
		want      error
	}{
		{"rejected", http.StatusUnauthorized, `{"error":"bad ` + verifierCanary + `"}`, nil, ErrKeyRejected},
		{"server", http.StatusInternalServerError, `{"error":"bad ` + verifierCanary + `"}`, nil, ErrKeyVerification},
		{"malformed", http.StatusOK, `{"data":{"label":"` + verifierCanary, nil, ErrKeyVerification},
		{"missing data", http.StatusOK, `{}`, nil, ErrKeyVerification},
		{"transport", 0, "", fmt.Errorf("dial failed for %s", verifierCanary), ErrKeyVerification},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			verifier := OpenRouterVerifier{
				Client: &http.Client{Transport: verifierTransport(func(*http.Request) (*http.Response, error) {
					if tt.transport != nil {
						return nil, tt.transport
					}
					return verifierResponse(tt.status, tt.body), nil
				})},
			}
			_, err := verifier.Verify(context.Background(), secret.New(verifierCanary))
			if !errors.Is(err, tt.want) {
				t.Errorf("Verify = %v, want %v", err, tt.want)
			}
			if err != nil && strings.Contains(err.Error(), verifierCanary) {
				t.Errorf("verification error leaked the credential: %v", err)
			}
		})
	}
}

func TestOpenRouterKeyVerifierNeverFollowsARedirect(t *testing.T) {
	var calls int
	verifier := OpenRouterVerifier{
		Client: &http.Client{Transport: verifierTransport(func(req *http.Request) (*http.Response, error) {
			calls++
			if calls > 1 {
				t.Fatalf("credential followed a redirect to %s", req.URL)
			}
			resp := verifierResponse(http.StatusFound, "redirect")
			resp.Header.Set("Location", "https://untrusted.invalid/steal")
			resp.Request = req
			return resp, nil
		})},
	}
	_, err := verifier.Verify(context.Background(), secret.New(verifierCanary))
	if !errors.Is(err, ErrKeyVerification) {
		t.Errorf("Verify redirect = %v, want ErrKeyVerification", err)
	}
	if calls != 1 {
		t.Errorf("verification made %d requests, want exactly 1", calls)
	}
}

func TestOpenRouterKeyVerifierRefusesAReplacementOriginBeforeNetwork(t *testing.T) {
	for _, tc := range replacementOrigins {
		if tc.baseURL == "" {
			continue // an empty BaseURL means the canonical default, by contract
		}
		t.Run(tc.name, func(t *testing.T) {
			var calls int
			verifier := OpenRouterVerifier{
				BaseURL: tc.baseURL,
				Client: &http.Client{Transport: verifierTransport(func(*http.Request) (*http.Response, error) {
					calls++
					return verifierResponse(http.StatusOK, `{"data":{}}`), nil
				})},
			}

			_, err := verifier.Verify(context.Background(), secret.New(verifierCanary))
			if err == nil {
				t.Fatal("OpenRouter verifier sent a credential to a replacement origin")
			}
			if calls != 0 {
				t.Fatalf("replacement transport was called %d times, want 0", calls)
			}
			if !errors.Is(err, ErrKeyVerification) {
				t.Fatalf("Verify error = %v, want ErrKeyVerification", err)
			}
			if strings.Contains(err.Error(), verifierCanary) {
				t.Fatalf("origin refusal leaked the credential: %v", err)
			}
		})
	}
}

func TestOpenRouterKeyVerifierAcceptsEveryCanonicalSpelling(t *testing.T) {
	for _, tc := range canonicalSpellings {
		t.Run(tc.name, func(t *testing.T) {
			var got *http.Request
			verifier := OpenRouterVerifier{
				BaseURL: tc.baseURL,
				Client: &http.Client{Transport: verifierTransport(func(r *http.Request) (*http.Response, error) {
					got = r
					return verifierResponse(http.StatusOK, `{"data":{"limit":1,"limit_remaining":1,"usage":0}}`), nil
				})},
			}
			if _, err := verifier.Verify(context.Background(), secret.New(verifierCanary)); err != nil {
				t.Fatalf("Verify(%q) = %v", tc.baseURL, err)
			}
			if got == nil {
				t.Fatal("canonical spelling never reached the transport")
			}
			if !strings.EqualFold(got.URL.Scheme, "https") || !strings.EqualFold(got.URL.Hostname(), "openrouter.ai") || got.URL.User != nil {
				t.Fatalf("credentialed verification went to %s", got.URL)
			}
			if got.Header.Get("Authorization") != "Bearer "+verifierCanary {
				t.Fatalf("Authorization = %q", got.Header.Get("Authorization"))
			}
		})
	}
}

func TestOpenRouterKeyVerifierHonorsCancellationAndRejectsEmptyValues(t *testing.T) {
	var calls int
	verifier := OpenRouterVerifier{
		Client: &http.Client{Transport: verifierTransport(func(*http.Request) (*http.Response, error) {
			calls++
			return verifierResponse(http.StatusOK, `{"data":{}}`), nil
		})},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := verifier.Verify(ctx, secret.New(verifierCanary)); !errors.Is(err, context.Canceled) {
		t.Errorf("Verify(canceled) = %v, want context.Canceled", err)
	}
	if _, err := verifier.Verify(context.Background(), secret.Secret{}); !errors.Is(err, ErrKeyRejected) {
		t.Errorf("Verify(empty) = %v, want ErrKeyRejected", err)
	}
	if calls != 0 {
		t.Errorf("invalid inputs made %d HTTP requests", calls)
	}
}

type verifierTransport func(*http.Request) (*http.Response, error)

func (f verifierTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func verifierResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
