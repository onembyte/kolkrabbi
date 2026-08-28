package engine

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

func TestNormalizeFreeExhausted(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", OnFreeExhaustedFree},
		{"free", OnFreeExhaustedFree},
		{" PAID ", OnFreeExhaustedPaid},
		{"stop", OnFreeExhaustedStop},
	} {
		got, err := NormalizeFreeExhausted(tc.in)
		if err != nil || got != tc.want {
			t.Fatalf("NormalizeFreeExhausted(%q) = (%q, %v), want %q", tc.in, got, err, tc.want)
		}
	}
	if _, err := NormalizeFreeExhausted("cheapest"); err == nil {
		t.Fatal("an unknown policy was accepted; want a rejection naming the three")
	}
}

// freeThenMetered answers only the metered model; every free one is
// rate-limited, which is what an exhausted free tier looks like from here.
type freeThenMetered struct {
	metered string
	models  []string
}

func (b *freeThenMetered) StreamChat(_ context.Context, model string, _ []provider.Message, _ []provider.Tool, _ func(string)) (provider.Message, provider.Meta, error) {
	b.models = append(b.models, model)
	if model == b.metered {
		return provider.Message{Role: "assistant", Content: "answered"}, provider.Meta{}, nil
	}
	return provider.Message{}, provider.Meta{}, &provider.HTTPError{StatusCode: http.StatusTooManyRequests}
}

func freeAgent(backend ChatBackend, policy string, out *strings.Builder) *Agent {
	return &Agent{Options: Options{
		Backend:         backend,
		Out:             out,
		Model:           "a/free:free",
		FreeModels:      []string{"a/free:free", "b/free:free"},
		OnFreeExhausted: policy,
		MeteredModel:    func() string { return "vendor/metered" },
		RetryWait:       func(context.Context, time.Duration) error { return nil },
	}}
}

// B12.13d. `stop` means no substitution at all: someone who set it would rather
// see the error than discover afterwards that three models answered one
// question.
func TestStopRefusesToRotateAtAll(t *testing.T) {
	backend := &freeThenMetered{metered: "vendor/metered"}
	out := &strings.Builder{}
	a := freeAgent(backend, OnFreeExhaustedStop, out)

	_, _, err := a.streamChat(context.Background(), "reply", "a/free:free", nil, nil, nil)
	if err == nil {
		t.Fatal("stop policy answered anyway, want a refusal")
	}
	if !strings.Contains(err.Error(), "routing.on_free_exhausted") {
		t.Errorf("error %q does not name the setting that changes it", err)
	}
	if len(backend.models) != 1 {
		t.Fatalf("tried %v, want only the model the caller asked for", backend.models)
	}
}

// `paid` is the opt-in: free is still tried first and exhausted first, and only
// then does anything cost money.
func TestPaidMovesToAMeteredModelOnlyAfterEveryFreeOneIsTried(t *testing.T) {
	backend := &freeThenMetered{metered: "vendor/metered"}
	out := &strings.Builder{}
	a := freeAgent(backend, OnFreeExhaustedPaid, out)

	msg, _, err := a.streamChat(context.Background(), "reply", "a/free:free", nil, nil, nil)
	if err != nil {
		t.Fatalf("streamChat: %v", err)
	}
	if msg.Content != "answered" {
		t.Fatalf("content = %q, want the metered model's answer", msg.Content)
	}
	tried := strings.Join(backend.models, " ")
	for _, free := range []string{"a/free:free", "b/free:free"} {
		if !strings.Contains(tried, free) {
			t.Errorf("moved to metered without trying %s; tried %v", free, backend.models)
		}
	}
	if backend.models[len(backend.models)-1] != "vendor/metered" {
		t.Errorf("ended on %q, want the metered model last", backend.models[len(backend.models)-1])
	}
	if !strings.Contains(out.String(), "billed") {
		t.Errorf("transcript %q does not say the run started costing money", out.String())
	}
}

// The default stays free. Every free model is tried and the run stops rather
// than reaching for the metered one that is sitting right there.
func TestFreeStaysFreeEvenWithAMeteredModelAvailable(t *testing.T) {
	backend := &freeThenMetered{metered: "vendor/metered"}
	out := &strings.Builder{}
	a := freeAgent(backend, OnFreeExhaustedFree, out)

	_, _, err := a.streamChat(context.Background(), "reply", "a/free:free", nil, nil, nil)
	if err == nil {
		t.Fatal("the default policy billed a metered model, want the run to stop")
	}
	for _, model := range backend.models {
		if model == "vendor/metered" {
			t.Fatalf("the default policy reached the metered model; tried %v", backend.models)
		}
	}
	if !strings.Contains(err.Error(), "routing.on_free_exhausted") {
		t.Errorf("error %q does not tell the user how to allow a paid fallback", err)
	}
}

// The policy must not shorten the bounded retries that a transient rate limit
// usually clears within. Rotation exists to give the last free model those
// retries, and an earlier version of this feature took them away.
func TestThePolicyDoesNotSkipTheBoundedRetries(t *testing.T) {
	attempts := 0
	backend := scriptedBackend(func(string) (provider.Message, error) {
		attempts++
		if attempts <= 3 {
			return provider.Message{}, &provider.HTTPError{StatusCode: http.StatusTooManyRequests}
		}
		return provider.Message{Role: "assistant", Content: "cleared"}, nil
	})
	out := &strings.Builder{}
	a := freeAgent(backend, OnFreeExhaustedPaid, out)

	msg, _, err := a.streamChat(context.Background(), "reply", "a/free:free", nil, nil, nil)
	if err != nil {
		t.Fatalf("streamChat: %v", err)
	}
	if msg.Content != "cleared" {
		t.Fatalf("content = %q, want the answer once the rate limit cleared", msg.Content)
	}
	if strings.Contains(out.String(), "billed") {
		t.Errorf("moved to a metered model while free models were still recovering:\n%s", out.String())
	}
}

// A pinned model never rotates, so nothing else free is ever tried — and a
// pinned *free* model quietly becoming a billed one is the sharpest version of
// the surprise this whole setting exists to prevent. Pinning is a decision;
// `paid` does not overrule it.
func TestAPinnedFreeModelIsNeverBilledInstead(t *testing.T) {
	backend := &freeThenMetered{metered: "vendor/metered"}
	out := &strings.Builder{}
	a := freeAgent(backend, OnFreeExhaustedPaid, out)
	a.PinnedModel = true

	_, _, err := a.streamChat(context.Background(), "reply", "a/free:free", nil, nil, nil)
	if err == nil {
		t.Fatal("a pinned free model was substituted, want the run to stop")
	}
	for _, model := range backend.models {
		if model == "vendor/metered" {
			t.Fatalf("a pinned free model became a billed one; tried %v", backend.models)
		}
	}
}
