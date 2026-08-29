package engine

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// hostLimitBackend answers the metered model and refuses the host one with an
// Ollama Cloud usage limit — the shape a proxied cloud 429 has: origin ollama,
// wording that A33.7's allowance phrases would otherwise match.
type hostLimitBackend struct {
	models []string
}

func (b *hostLimitBackend) StreamChat(_ context.Context, model string, _ []provider.Message, _ []provider.Tool, _ func(string)) (provider.Message, provider.Meta, error) {
	b.models = append(b.models, model)
	if model == "openai/gpt-5.6-luna" {
		return provider.Message{Role: "assistant", Content: "billed"}, provider.Meta{}, nil
	}
	return provider.Message{}, provider.Meta{}, &provider.HTTPError{
		StatusCode: http.StatusTooManyRequests,
		Origin:     provider.HostOrigin,
		Message:    "you have reached your session usage limit",
	}
}

func hostAgent(backend *hostLimitBackend, waits *int) *Agent {
	return &Agent{Options: Options{
		Backend:             backend,
		Routes:              map[string]ChatBackend{"ollama": backend},
		Out:                 &strings.Builder{},
		Model:               "ollama/gpt-oss:120b-cloud",
		OnSubscriptionLimit: OnLimitSwitch,
		OnFreeExhausted:     OnFreeExhaustedPaid,
		MeteredModel:        func() string { return "openai/gpt-5.6-luna" },
		FreeModels:          []string{"a/free:free"},
		RetryWait:           func(context.Context, time.Duration) error { *waits++; return nil },
	}}
}

// E7. Ollama Cloud's limit resets — session limits every 5 h, weekly ones every
// 7 d. It is not A33.7's exhausted plan, which never clears by waiting, and it
// must not trip the metered fallback even under `switch`: a local or cloud
// model is its own cost class, governed by neither policy.
func TestAHostLimitNeverTriggersTheSubscriptionPolicy(t *testing.T) {
	backend := &hostLimitBackend{}
	waits := 0
	a := hostAgent(backend, &waits)

	_, _, err := a.streamChat(context.Background(), "reply", "ollama/gpt-oss:120b-cloud", nil, nil, nil)
	if err == nil {
		t.Fatal("a cloud usage limit was answered; want the turn to stop")
	}
	for _, model := range backend.models {
		if model == "openai/gpt-5.6-luna" {
			t.Fatalf("a host limit moved the run to a billed model: %v", backend.models)
		}
	}
	if !strings.Contains(err.Error(), "resets") {
		t.Errorf("error %q does not say the limit resets", err)
	}
	if strings.Contains(err.Error(), "out of allowance") || strings.Contains(err.Error(), "on_subscription_limit") {
		t.Errorf("error %q reads a resetting limit as an exhausted plan", err)
	}
}

// A limit that resets in hours is not cleared by a 1-2-4 second backoff, and
// a local server is not a rate-limited gateway. One attempt, no waiting, no
// rotation.
func TestAHostLimitIsNotRetriedOrRotated(t *testing.T) {
	backend := &hostLimitBackend{}
	waits := 0
	a := hostAgent(backend, &waits)

	_, _, _ = a.streamChat(context.Background(), "reply", "ollama/gpt-oss:120b-cloud", nil, nil, nil)
	if len(backend.models) != 1 {
		t.Fatalf("made %d attempts, want 1: %v", len(backend.models), backend.models)
	}
	if waits != 0 {
		t.Fatalf("waited %d times on a limit that resets in hours", waits)
	}
	if strings.Contains(a.Out.(*strings.Builder).String(), "rotating") {
		t.Fatal("a host model was rotated as if it were a free gateway model")
	}
}
