package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/protocol"
)

func pausedAgent(t *testing.T, out *bytes.Buffer, switching func(context.Context, continuity.Candidate) (string, error)) *Agent {
	t.Helper()
	srv := enginetest.New(enginetest.Step{StatusCode: http.StatusTooManyRequests, RetryAfter: "1800", ErrorBody: `{"error":{"message":"rate limited"}}`})
	t.Cleanup(srv.Close)
	a := New(Options{
		Client: provider.NewCompatibleClient(srv.URL), Bus: newTestBus(t), Mode: ModeCode, Model: "claude-fable", PinnedModel: true, Effort: EffortMax,
		Permission: PermissionFullAuto, Out: out, Sess: enginetest.NewFakeSession("s_test", "claude-fable"),
		ConnectorName: func(string) string { return "claude" },
		Candidates: func() []continuity.Candidate {
			return []continuity.Candidate{
				{Model: "gpt-5.6-sol", Connector: "codex", Plan: "ChatGPT Plus", Billing: "subscription", Rating: 4.5, Ratings: 3},
				{Model: "gemini-2.5-pro", Connector: "google", Billing: "api-metered"},
				{Model: "gpt-5.6-luna", Connector: "codex", Plan: "ChatGPT Plus", Billing: "subscription"},
			}
		},
		Switch: switching,
	})
	if err := a.RunTurn(context.Background(), "finish the report"); err == nil {
		t.Fatal("the limit did not pause")
	}
	return a
}

// V35.4a, plan 35 §2.5: when the person says continue, the chain is walked
// in the recommendation's order — the surface switches the session to the
// next equivalent, a hop that fails is set aside and the next is tried, and
// the turn that was waiting comes back to run on the new model with the
// pause lifted. Every hop is printed and published as a switch; nothing is
// persisted about it.
func TestContinueWalksTheChainAndHandsBackThePendingTurn(t *testing.T) {
	var out bytes.Buffer
	var tried []string
	a := pausedAgent(t, &out, func(_ context.Context, c continuity.Candidate) (string, error) {
		tried = append(tried, c.Connector+"/"+c.Model)
		if c.Model == "gpt-5.6-sol" {
			return "", errors.New("codex is not signed in")
		}
		return c.Model + " (via key)", nil
	})
	pending, chosen, err := a.ContinueOn(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if pending != "finish the report" || chosen.Model != "gemini-2.5-pro" {
		t.Fatalf("continue = %q on %+v, want the pending turn on the second equivalent", pending, chosen)
	}
	if strings.Join(tried, " ") != "codex/gpt-5.6-sol google/gemini-2.5-pro" {
		t.Fatalf("hops = %v, want the chain in order until one took", tried)
	}
	if a.Sess.Paused() != nil {
		t.Fatal("the pause was not lifted after the switch")
	}
	text := out.String()
	if !strings.Contains(text, "continuing on gemini-2.5-pro (via key) at max (API key, metered)") || !strings.Contains(text, "gpt-5.6-sol could not take over") {
		t.Fatalf("console did not say the hop and the failed one:\n%s", text)
	}
	switches := 0
	for _, env := range bReplay(t, a.Bus) {
		if env.Type != protocol.EventProviderLimit {
			continue
		}
		var data protocol.ProviderLimitData
		_ = json.Unmarshal(env.Data, &data)
		if data.Action == "switch" {
			switches++
		}
	}
	if switches != 1 {
		t.Fatalf("switch events = %d, want one", switches)
	}
}

// Picking the second equivalent by number starts the walk there; a chain
// with nothing left says so and keeps the pause.
func TestContinueByNumberAndWithNothingLeft(t *testing.T) {
	var out bytes.Buffer
	a := pausedAgent(t, &out, func(_ context.Context, c continuity.Candidate) (string, error) { return c.Model, nil })
	_, chosen, err := a.ContinueOn(context.Background(), 1)
	if err != nil || chosen.Model != "gemini-2.5-pro" {
		t.Fatalf("continue 2 = %+v, %v; want the second equivalent", chosen, err)
	}
	var out2 bytes.Buffer
	b := pausedAgent(t, &out2, func(_ context.Context, c continuity.Candidate) (string, error) { return "", errors.New("no") })
	_, _, err = b.ContinueOn(context.Background(), 0)
	if err == nil || !strings.Contains(err.Error(), "nothing") {
		t.Fatalf("exhausted chain err = %v, want 'nothing'", err)
	}
	if b.Sess.Paused() == nil {
		t.Fatal("an exhausted chain lifted the pause")
	}
}
