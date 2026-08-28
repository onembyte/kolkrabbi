package engine

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

func TestNormalizeSubscriptionLimitAcceptsThePolicies(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", OnLimitAsk},
		{"ask", OnLimitAsk},
		{" Switch ", OnLimitSwitch},
		{"STOP", OnLimitStop},
	} {
		got, err := NormalizeSubscriptionLimit(tc.in)
		if err != nil {
			t.Fatalf("NormalizeSubscriptionLimit(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("NormalizeSubscriptionLimit(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if _, err := NormalizeSubscriptionLimit("continue"); err == nil {
		t.Fatal("NormalizeSubscriptionLimit(\"continue\") = nil error, want a rejection naming the three policies")
	}
}

func TestSubscriptionLimitedSeparatesAllowanceFromRateLimit(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"out of credit", &provider.HTTPError{StatusCode: http.StatusPaymentRequired}, true},
		{"plan allowance", &provider.HTTPError{StatusCode: http.StatusTooManyRequests, LimitSource: "subscription"}, true},
		{"per-minute rate limit", &provider.HTTPError{StatusCode: http.StatusTooManyRequests}, false},
		{"cli usage limit", errors.New("claude: usage limit reached; resets at 4pm"), true},
		{"ordinary failure", errors.New("connection reset by peer"), false},
		{"nil", nil, false},
	} {
		if got := subscriptionLimited(tc.err); got != tc.want {
			t.Fatalf("subscriptionLimited(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// stubChooser answers a Choice with a fixed option, recording the question so a
// test can prove the person was actually asked before money was spent.
type stubChooser struct {
	answer string
	ok     bool
	asked  []Choice
}

func (s *stubChooser) Choose(_ context.Context, c Choice) (string, bool) {
	s.asked = append(s.asked, c)
	return s.answer, s.ok
}

func TestSubscriptionLimitDecision(t *testing.T) {
	const metered = "openai/gpt-5.6-luna"

	t.Run("stop never switches", func(t *testing.T) {
		a := &Agent{Options: Options{Ask: &stubChooser{answer: "Continue on " + metered, ok: true}}}
		if next, ok := a.afterSubscriptionLimit(context.Background(), OnLimitStop, metered); ok {
			t.Fatalf("stop returned %q, want no continuation", next)
		}
		if asked := len(a.Ask.(*stubChooser).asked); asked != 0 {
			t.Fatalf("stop asked %d questions, want 0", asked)
		}
	})

	t.Run("switch continues without asking", func(t *testing.T) {
		chooser := &stubChooser{}
		a := &Agent{Options: Options{Ask: chooser}}
		next, ok := a.afterSubscriptionLimit(context.Background(), OnLimitSwitch, metered)
		if !ok || next != metered {
			t.Fatalf("switch = (%q, %v), want (%q, true)", next, ok, metered)
		}
		if len(chooser.asked) != 0 {
			t.Fatalf("switch asked %d questions, want 0: switch is the answer already given", len(chooser.asked))
		}
	})

	t.Run("ask spends only on a yes", func(t *testing.T) {
		chooser := &stubChooser{answer: "Continue on " + metered, ok: true}
		a := &Agent{Options: Options{Ask: chooser}}
		next, ok := a.afterSubscriptionLimit(context.Background(), OnLimitAsk, metered)
		if !ok || next != metered {
			t.Fatalf("ask+yes = (%q, %v), want (%q, true)", next, ok, metered)
		}
		if len(chooser.asked) != 1 {
			t.Fatalf("asked %d times, want exactly 1", len(chooser.asked))
		}
		if q := chooser.asked[0].Question; !strings.Contains(q, metered) {
			t.Fatalf("question %q does not name the model that would be billed", q)
		}
	})

	t.Run("ask stops on a no", func(t *testing.T) {
		a := &Agent{Options: Options{Ask: &stubChooser{answer: "Stop here", ok: true}}}
		if next, ok := a.afterSubscriptionLimit(context.Background(), OnLimitAsk, metered); ok {
			t.Fatalf("ask+no returned %q, want no continuation", next)
		}
	})

	t.Run("ask with nobody there stops rather than hanging or spending", func(t *testing.T) {
		a := &Agent{Options: Options{Ask: nil}}
		if next, ok := a.afterSubscriptionLimit(context.Background(), OnLimitAsk, metered); ok {
			t.Fatalf("ask with no chooser returned %q, want no continuation: nobody can consent to a bill", next)
		}
	})

	t.Run("no metered fallback is a stop whatever the policy", func(t *testing.T) {
		for _, policy := range []string{OnLimitAsk, OnLimitSwitch, OnLimitStop} {
			a := &Agent{Options: Options{Ask: &stubChooser{answer: "yes", ok: true}}}
			if next, ok := a.afterSubscriptionLimit(context.Background(), policy, ""); ok {
				t.Fatalf("%s with no fallback returned %q, want no continuation", policy, next)
			}
		}
	})
}

// limitThenOKBackend fails the first call with an exhausted allowance and
// answers every later one, recording which model each call named.
type limitThenOKBackend struct {
	limited string
	models  []string
}

func (b *limitThenOKBackend) StreamChat(_ context.Context, model string, _ []provider.Message, _ []provider.Tool, _ func(string)) (provider.Message, provider.Meta, error) {
	b.models = append(b.models, model)
	if model == b.limited {
		return provider.Message{}, provider.Meta{}, &provider.HTTPError{
			StatusCode:  http.StatusTooManyRequests,
			LimitSource: "subscription",
		}
	}
	return provider.Message{Role: "assistant", Content: "done"}, provider.Meta{}, nil
}

func TestStreamChatMovesToMeteredOnlyWhenAllowed(t *testing.T) {
	const plan, metered = "anthropic/claude-sonnet-4-5", "openai/gpt-5.6-luna"

	t.Run("switch continues the turn and says so", func(t *testing.T) {
		backend := &limitThenOKBackend{limited: plan}
		out := &strings.Builder{}
		a := &Agent{Options: Options{
			Backend:             backend,
			Out:                 out,
			Model:               plan,
			OnSubscriptionLimit: OnLimitSwitch,
			MeteredModel:        func() string { return metered },
		}}
		msg, _, err := a.streamChat(context.Background(), "reply", plan, nil, nil, nil)
		if err != nil {
			t.Fatalf("streamChat: %v", err)
		}
		if msg.Content != "done" {
			t.Fatalf("content = %q, want the answer from the metered model", msg.Content)
		}
		if want := []string{plan, metered}; len(backend.models) != 2 || backend.models[1] != metered {
			t.Fatalf("models called = %v, want %v", backend.models, want)
		}
		if a.Model != metered {
			t.Fatalf("a.Model = %q, want the session moved to %q", a.Model, metered)
		}
		if !strings.Contains(out.String(), metered) || !strings.Contains(out.String(), "billed per token") {
			t.Fatalf("transcript %q does not say the run moved onto a billed model", out.String())
		}
	})

	t.Run("ask with nobody there stops and names the way out", func(t *testing.T) {
		backend := &limitThenOKBackend{limited: plan}
		a := &Agent{Options: Options{
			Backend:             backend,
			Out:                 &strings.Builder{},
			Model:               plan,
			OnSubscriptionLimit: OnLimitAsk,
			MeteredModel:        func() string { return metered },
		}}
		_, _, err := a.streamChat(context.Background(), "reply", plan, nil, nil, nil)
		if err == nil {
			t.Fatal("streamChat succeeded; want the run stopped rather than billed with nobody to ask")
		}
		if !strings.Contains(err.Error(), "routing.on_subscription_limit") {
			t.Fatalf("error %q does not name the setting that changes this", err)
		}
		if len(backend.models) != 1 {
			t.Fatalf("models called = %v, want only the plan model: nothing may be billed unasked", backend.models)
		}
	})

	t.Run("one question for the whole session", func(t *testing.T) {
		chooser := &stubChooser{answer: "Continue on " + metered, ok: true}
		a := &Agent{Options: Options{
			Backend:             &limitThenOKBackend{limited: plan},
			Out:                 &strings.Builder{},
			Model:               plan,
			Ask:                 chooser,
			OnSubscriptionLimit: OnLimitAsk,
			MeteredModel:        func() string { return metered },
		}}
		for range 3 {
			if _, _, err := a.streamChat(context.Background(), "reply", plan, nil, nil, nil); err != nil {
				t.Fatalf("streamChat: %v", err)
			}
		}
		if len(chooser.asked) != 1 {
			t.Fatalf("asked %d times across three turns, want 1: the allowance belongs to the session", len(chooser.asked))
		}
	})
}
