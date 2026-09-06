package engine

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

type answeringBackend struct{ prompts []string }

func (b *answeringBackend) StreamChat(_ context.Context, _ string, messages []provider.Message, _ []provider.Tool, _ func(string)) (provider.Message, provider.Meta, error) {
	b.prompts = append(b.prompts, messages[len(messages)-1].Content)
	return provider.Message{Role: "assistant", Content: "done on the next model"}, provider.Meta{Model: "gpt-5.6-sol", Billing: provider.BillingSubscription}, nil
}

type fixedChooser struct {
	answer string
	asked  int
	seen   Choice
}

func (c *fixedChooser) Choose(_ context.Context, choice Choice) (string, bool) {
	c.asked++
	c.seen = choice
	if c.answer == "" {
		return "", false
	}
	return c.answer, true
}

func limitedAgent(t *testing.T, out *bytes.Buffer, mode, selection string, chooser Chooser, next *answeringBackend) *Agent {
	t.Helper()
	srv := enginetest.New(enginetest.Step{StatusCode: http.StatusTooManyRequests, RetryAfter: "1800", ErrorBody: `{"error":{"message":"rate limited"}}`})
	t.Cleanup(srv.Close)
	var a *Agent
	a = New(Options{
		Client: provider.NewCompatibleClient(srv.URL), Bus: newTestBus(t), Mode: ModeChat, Model: "claude-fable", PinnedModel: true, Effort: EffortMax,
		Permission: PermissionFullAuto, Out: out, Sess: enginetest.NewFakeSession("s_test", "claude-fable"),
		ConnectorName:  func(string) string { return "claude" },
		ContinuityMode: mode, Select: selection, Ask: chooser,
		Candidates: func() []continuity.Candidate {
			return []continuity.Candidate{
				{Model: "gpt-5.6-sol", Connector: "codex", Plan: "ChatGPT Plus", Billing: "subscription", Rating: 4.5, Ratings: 3},
				{Model: "gemini-2.5-pro", Connector: "google", Billing: "api-metered"},
			}
		},
		Switch: func(_ context.Context, c continuity.Candidate) (string, error) {
			a.SetSessionBackend(next)
			a.SetSessionModel(c.Model)
			return c.Ref(), nil
		},
	})
	return a
}

// V35.6, plan 35 §2.4 `mode on, select auto`: a limit pauses, the block is
// printed, and then the chain is walked by itself — the session switches to
// the best equivalent and the turn that was waiting runs there, in the same
// turn, so the person sees the answer, the hop printed above it, and no
// pause left behind. Off, the default, stays a pause.
func TestModeOnWalksTheChainByItselfAndFinishesTheTurn(t *testing.T) {
	var out bytes.Buffer
	next := &answeringBackend{}
	a := limitedAgent(t, &out, "on", "auto", nil, next)
	if err := a.RunTurn(context.Background(), "finish the report"); err != nil {
		t.Fatalf("mode on: RunTurn = %v, want the turn finished on the next model", err)
	}
	if len(next.prompts) != 1 || !strings.Contains(next.prompts[0], "finish the report") {
		t.Fatalf("next model got %v, want the waiting turn", next.prompts)
	}
	if a.Sess.Paused() != nil {
		t.Fatal("a pause was left behind after the automatic hop")
	}
	if !strings.Contains(out.String(), "continuing on ChatGPT Plus/gpt-5.6-sol at max (subscription)") {
		t.Fatalf("the hop was not printed:\n%s", out.String())
	}

	var off bytes.Buffer
	stays := limitedAgent(t, &off, "off", "auto", nil, &answeringBackend{})
	if err := stays.RunTurn(context.Background(), "go"); err == nil || stays.Sess.Paused() == nil {
		t.Fatalf("mode off: err = %v paused=%v; want a pause", err, stays.Sess.Paused() != nil)
	}
}

// `select ask`: the block becomes a question — the top equivalent, the next,
// and "pause and resume later" — asked once per run; the answer is walked
// from, and declining keeps the pause.
func TestSelectAskAsksOnceAndWalksFromTheAnswer(t *testing.T) {
	var out bytes.Buffer
	next := &answeringBackend{}
	chooser := &fixedChooser{answer: "gemini-2.5-pro (API key, metered)"}
	a := limitedAgent(t, &out, "on", "ask", chooser, next)
	if err := a.RunTurn(context.Background(), "finish"); err != nil {
		t.Fatalf("ask then continue: %v", err)
	}
	if chooser.asked != 1 || len(chooser.seen.Options) != 3 || !strings.Contains(chooser.seen.Options[2], "pause") {
		t.Fatalf("asked %d with %v, want once with top, next and pause", chooser.asked, chooser.seen.Options)
	}
	if a.SessionModel() != "gemini-2.5-pro" {
		t.Fatalf("session moved to %q, want the chosen one", a.SessionModel())
	}

	var kept bytes.Buffer
	decline := &fixedChooser{answer: "pause and resume later"}
	b := limitedAgent(t, &kept, "on", "ask", decline, &answeringBackend{})
	if err := b.RunTurn(context.Background(), "go"); err == nil || b.Sess.Paused() == nil {
		t.Fatal("declining did not keep the pause")
	}
	// Asked once per run: a second limited turn in the same run does not ask again.
	_ = b.RunTurn(context.Background(), "again")
	if decline.asked != 1 {
		t.Fatalf("asked %d times in one run, want once", decline.asked)
	}
}
