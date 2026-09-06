package continuity

import (
	"strings"
	"testing"
)

// ladder is the vendor rung table as the engine keeps it: index 0 is the
// top, and a gateway id such as anthropic/claude-opus-4 ranks by the rung
// its tail starts with, as the engine's modelRank does.
func ladder(model string) (string, int, bool) {
	rungs := []struct {
		ladder string
		rungs  []string
	}{
		{"claude", []string{"claude-fable", "claude-opus", "claude-sonnet", "claude-haiku"}},
		{"codex", []string{"gpt-5.6-pro", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"}},
		{"gemini", []string{"gemini-2.5-ultra", "gemini-2.5-pro", "gemini-2.5-flash"}},
	}
	tail := model
	if i := strings.LastIndex(model, "/"); i >= 0 {
		tail = model[i+1:]
	}
	for _, l := range rungs {
		for i, rung := range l.rungs {
			if strings.HasPrefix(tail, rung) {
				return l.ladder, i, true
			}
		}
	}
	return "", 0, false
}

// V35.3a, plan 35 §2.3 and the owner's answers: on a limit, what could
// continue the work. Eligible means enabled, not cooling for its scope and
// fit for the task; equivalent means the same rung of the vendor ladder, or
// one above, or one below — never further below, never a free model unless
// the person put it on the preferred list. Subscriptions first, then paid,
// then free; within a group, the person's own ratings.
func TestRecommendRanksEquivalentsSubscriptionsFirstThenByRating(t *testing.T) {
	current := Candidate{Model: "claude-fable", Connector: "claude", Plan: "Claude Max", Billing: "subscription"}
	candidates := []Candidate{
		{Model: "claude-opus", Connector: "claude", Plan: "Claude Max", Billing: "subscription"},                             // same account: cooling
		{Model: "gpt-5.6-luna", Connector: "codex", Plan: "ChatGPT Plus", Billing: "subscription"},                           // three rungs below
		{Model: "gpt-5.6-sol", Connector: "codex", Plan: "ChatGPT Plus", Billing: "subscription", Rating: 4.5, Ratings: 3},   // one below, rated
		{Model: "gpt-5.6-terra", Connector: "codex", Plan: "ChatGPT Plus", Billing: "subscription", Rating: 4.8, Ratings: 1}, // two below
		{Model: "auto", Exact: "gpt-5.6-luna", Connector: "copilot", Plan: "Copilot Free", Billing: "subscription"},          // ranked by what auto chose
		{Model: "gemini-2.5-pro", Connector: "google", Billing: "api-metered", Rating: 4.0, Ratings: 2},                      // one below, paid
		{Model: "grok-4.6", Connector: "xai", Billing: "api-metered"},                                                        // no rung known
		{Model: "qwen/qwen3-coder:free", Connector: "openrouter", Billing: "gateway", Free: true},                            // free, not preferred
		{Model: "anthropic/claude-opus-4", Connector: "openrouter", Billing: "gateway", Rating: 5, Ratings: 9},               // gateway paid; ranks as opus (one below)
	}
	cooling := func(connector, model string) bool { return connector == "claude" }
	rec := Recommend(current, Need{}, candidates, nil, cooling, ladder)

	var got []string
	for _, c := range rec.Equivalent {
		got = append(got, c.Connector+"/"+c.Model)
	}
	want := []string{"codex/gpt-5.6-sol", "openrouter/anthropic/claude-opus-4", "google/gemini-2.5-pro"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("equivalent = %v, want %v", got, want)
	}
	if rec.Top == nil || rec.Top.Model != "gpt-5.6-sol" {
		t.Fatalf("top = %+v, want the rated subscription rung", rec.Top)
	}
	why := map[string]string{}
	for _, x := range rec.Excluded {
		why[x.Candidate.Connector+"/"+x.Candidate.Model] = x.Why
	}
	for key, wantWhy := range map[string]string{
		"claude/claude-opus":               "cooling",
		"codex/gpt-5.6-luna":               "rungs below",
		"copilot/auto":                     "rungs below",
		"codex/gpt-5.6-terra":              "rungs below",
		"xai/grok-4.6":                     "no rung",
		"openrouter/qwen/qwen3-coder:free": "free",
	} {
		if !strings.Contains(why[key], wantWhy) {
			t.Fatalf("%s excluded for %q, want %q (all: %v)", key, why[key], wantWhy, why)
		}
	}
}

// A preferred free model is equivalent by the person's word, last in line;
// a task that needs tools excludes a candidate known to lack them; a task
// bigger than a candidate's context excludes it; the current model itself
// is never a recommendation; and with the owner's order flipped, paid goes
// before subscriptions.
func TestRecommendHonoursPreferenceCapabilityAndOrder(t *testing.T) {
	current := Candidate{Model: "claude-fable", Connector: "claude", Billing: "subscription"}
	candidates := []Candidate{
		{Model: "claude-fable", Connector: "claude", Billing: "subscription"},
		{Model: "gpt-5.6-sol", Connector: "codex", Billing: "subscription", LacksTools: true},
		{Model: "gpt-5.6-pro", Connector: "codex", Billing: "subscription", Context: 32000},
		{Model: "gemini-2.5-pro", Connector: "google", Billing: "api-metered"},
		{Model: "qwen/qwen3-coder:free", Connector: "openrouter", Billing: "gateway", Free: true, Preferred: true},
	}
	rec := Recommend(current, Need{Tools: true, Context: 100000}, candidates, []string{"paid", "subscription", "free"}, nil, ladder)
	var got []string
	for _, c := range rec.Equivalent {
		got = append(got, c.Model)
	}
	if strings.Join(got, " ") != "gemini-2.5-pro qwen/qwen3-coder:free" {
		t.Fatalf("equivalent = %v", got)
	}
	why := map[string]string{}
	for _, x := range rec.Excluded {
		why[x.Candidate.Model] = x.Why
	}
	if !strings.Contains(why["gpt-5.6-sol"], "tools") || !strings.Contains(why["gpt-5.6-pro"], "context") || !strings.Contains(why["claude-fable"], "stopped") {
		t.Fatalf("exclusions = %v", why)
	}
}

// A current model on no ladder has no equivalents except preferred ones, and
// the recommendation says why rather than inventing a peer.
func TestRecommendWithAnUnrankedCurrentModelOnlyOffersPreferred(t *testing.T) {
	current := Candidate{Model: "grok-4.6", Connector: "xai", Billing: "api-metered"}
	rec := Recommend(current, Need{}, []Candidate{
		{Model: "gpt-5.6-sol", Connector: "codex", Billing: "subscription"},
		{Model: "claude-opus", Connector: "claude", Billing: "subscription", Preferred: true},
	}, nil, nil, ladder)
	if len(rec.Equivalent) != 1 || rec.Equivalent[0].Model != "claude-opus" || !strings.Contains(rec.Note, "no rung") {
		t.Fatalf("rec = %+v", rec)
	}
}
