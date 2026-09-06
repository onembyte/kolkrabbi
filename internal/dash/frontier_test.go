package dash

import (
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/stats"
)

// The frontier share: the tokens that ran below the highest rung the log
// used, as a share of all of them — a log on one rung says nothing.
func TestTheDashSaysWhatShareRanBelowTheTopRung(t *testing.T) {
	page := Page([]stats.Record{
		{Kind: "call", Time: day(1), Turn: "t1", Model: "big", Effort: "max", PromptTokens: 800, CompletionTokens: 200, Cost: 1},
		{Kind: "call", Time: day(1), Turn: "t2", Model: "small", Effort: "low", PromptTokens: 2500, CompletionTokens: 500, Cost: 0.1},
		{Kind: "call", Time: day(1), Turn: "t3", Model: "mid", Effort: "medium", PromptTokens: 900, CompletionTokens: 100, Cost: 0.2},
	}, 0, nil, nil)
	if !strings.Contains(page, "80% of") || !strings.Contains(page, "below your top rung (max)") {
		t.Fatalf("frontier share missing or wrong:\n%s", page)
	}
	one := Page([]stats.Record{{Kind: "call", Time: day(1), Turn: "t1", Model: "big", Effort: "max", PromptTokens: 10, Cost: 1}}, 0, nil, nil)
	if strings.Contains(one, "below your top rung") {
		t.Fatal("a one-rung log claimed a share")
	}
}
