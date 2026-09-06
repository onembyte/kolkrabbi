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

// V35.3b: on a limit, one block says which backend stopped and why, that kolk
// resumes at the reset, the best equivalent with its billing path and the
// command that applies it, and what was set aside and why. Nothing is
// applied. Today the message was the same sentence for every model.
func TestAPausePrintsTheRecommendationBlock(t *testing.T) {
	srv := enginetest.New(enginetest.Step{StatusCode: http.StatusTooManyRequests, RetryAfter: "1800", ErrorBody: `{"error":{"message":"rate limited"}}`})
	defer srv.Close()
	var out bytes.Buffer
	a := New(Options{
		Client: provider.NewCompatibleClient(srv.URL), Mode: ModeCode, Model: "claude-fable", PinnedModel: true,
		Permission: PermissionFullAuto, Out: &out, Sess: enginetest.NewFakeSession("s_test", "claude-fable"),
		ConnectorName: func(string) string { return "claude" },
		Candidates: func() []continuity.Candidate {
			return []continuity.Candidate{
				{Model: "gpt-5.6-sol", Connector: "codex", Plan: "ChatGPT Plus", Billing: "subscription", Rating: 4.5, Ratings: 3},
				{Model: "gpt-5.6-luna", Connector: "codex", Plan: "ChatGPT Plus", Billing: "subscription"},
				{Model: "qwen/qwen3-coder:free", Connector: "openrouter", Billing: "gateway", Free: true},
			}
		},
	})
	if err := a.RunTurn(context.Background(), "please do the thing"); err == nil {
		t.Fatal("a 30-minute limit did not pause")
	}
	text := out.String()
	for _, want := range []string{
		"claude-fable", "capacity limit",
		"Equivalent now:", "gpt-5.6-sol on ChatGPT Plus (subscription, rated 4.5★ ×3)", `/model "ChatGPT Plus/gpt-5.6-sol"`,
		"Set aside:", "gpt-5.6-luna (3 rungs below)", "qwen/qwen3-coder:free (free",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("block omits %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "routing.on_subscription_limit switch") {
		t.Fatalf("the old one-sentence advice is still printed:\n%s", text)
	}
}

// Without candidates the block still says what stopped and that kolk
// resumes — and that nothing configured could continue it.
func TestAPauseWithoutCandidatesSaysSo(t *testing.T) {
	srv := enginetest.New(enginetest.Step{StatusCode: http.StatusTooManyRequests, RetryAfter: "1800", ErrorBody: `{"error":{"message":"rate limited"}}`})
	defer srv.Close()
	var out bytes.Buffer
	a := New(Options{
		Client: provider.NewCompatibleClient(srv.URL), Mode: ModeCode, Model: "claude-fable", PinnedModel: true,
		Permission: PermissionFullAuto, Out: &out, Sess: enginetest.NewFakeSession("s_test", "claude-fable"),
	})
	_ = a.RunTurn(context.Background(), "go")
	if !strings.Contains(out.String(), "nothing else configured could continue") {
		t.Fatalf("block:\n%s", out.String())
	}
}
