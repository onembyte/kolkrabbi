package engine

import "strings"

// The model the user selected is a ceiling, not a hint.
//
// Orchestration routes downward freely — that is the point of the slots, and
// running a commit or an mkdir on the cheapest model is how a subscription
// lasts the day. It must never route upward. Someone who selects Sonnet has
// chosen what their allowance is spent at; handing a task to Opus because a
// ranking thought it would do better spends their plan faster than they agreed
// to, and they did not ask.
//
// This is a filter in code rather than a line in the system prompt on purpose.
// A prompt is a request — a model reading "prefer cheaper models" may decide,
// reasonably, that this particular task deserves the strong one. A filter is a
// guarantee, and a guarantee is what a spending limit has to be.

// vendorLadder is one vendor's models from most capable to least.
//
// Ordered by what the vendor itself says: Claude's own picker calls Fable "most
// capable for your hardest and longest-running tasks", Sonnet "efficient for
// routine tasks", and Haiku "fastest for quick answers".
//
// Matching is by prefix, so `claude-sonnet`, `claude-sonnet-5` and
// `anthropic/claude-sonnet-4` all land on the same rung. A model on no ladder
// has no rank, and an unranked model is never clamped: a ceiling that guessed
// would be worse than one that admits it does not know.
type vendorLadder struct {
	name  string
	rungs []string
}

var vendorLadders = []vendorLadder{
	{name: "claude", rungs: []string{"claude-fable", "claude-opus", "claude-sonnet", "claude-haiku"}},
	{name: "codex", rungs: []string{"gpt-5.6-pro", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"}},
	{name: "gemini", rungs: []string{"gemini-2.5-ultra", "gemini-2.5-pro", "gemini-2.5-flash"}},
}

// modelRank locates a model on its vendor's ladder. Lower rung means more
// capable, so a smaller number is a stronger model.
func modelRank(model string) (ladder string, rung int, known bool) {
	normalized := strings.ToLower(strings.TrimSpace(model))
	if normalized == "" {
		return "", 0, false
	}
	// Ids arrive in three shapes: a bare plan model (`claude-haiku`), a
	// catalogue id carrying its provider (`anthropic/claude-haiku`), and a
	// namespaced route (`claude/haiku`). All three name the same rung, so all
	// three are tried — matching only the tail would leave `claude/haiku`
	// unranked, and an unranked model is never clamped, which would make a
	// whole id namespace invisible to the ceiling.
	forms := []string{normalized}
	if slash := strings.LastIndex(normalized, "/"); slash >= 0 {
		forms = append(forms,
			normalized[slash+1:],
			// `claude/haiku` -> `claude-haiku`: the namespace IS the vendor.
			normalized[:slash]+"-"+normalized[slash+1:])
	}
	best, bestLadder, found := 0, "", false
	for _, candidate := range vendorLadders {
		for index, rung := range candidate.rungs {
			if !matchesAnyForm(forms, rung) {
				continue
			}
			// Longest match wins, so `claude-opus` does not shadow a future
			// `claude-opus-mini` sitting lower on the ladder.
			if !found || len(rung) > len(candidate.rungs[best]) || bestLadder != candidate.name {
				best, bestLadder, found = index, candidate.name, true
			}
		}
	}
	if !found {
		return "", 0, false
	}
	return bestLadder, best, true
}

// matchesAnyForm reports whether any spelling of an id names this rung.
func matchesAnyForm(forms []string, rung string) bool {
	for _, form := range forms {
		if strings.HasPrefix(form, rung) {
			return true
		}
	}
	return false
}

// ClampToCeiling returns chosen, or ceiling when chosen is the more capable of
// the two.
//
// Only within one vendor: a ceiling of `claude-sonnet` says nothing about an
// OpenRouter model, because that is a different bill entirely, and silently
// rewriting a model the user configured for another provider would be a
// surprise of its own. Cross-vendor is left alone deliberately.
func ClampToCeiling(chosen, ceiling string) string {
	if strings.TrimSpace(chosen) == "" {
		return ceiling
	}
	chosenLadder, chosenRung, chosenKnown := modelRank(chosen)
	ceilingLadder, ceilingRung, ceilingKnown := modelRank(ceiling)
	if !chosenKnown || !ceilingKnown || chosenLadder != ceilingLadder {
		return chosen
	}
	if chosenRung < ceilingRung {
		// chosen is above the ceiling: take the ceiling instead.
		return ceiling
	}
	return chosen
}

// LadderRungIDs is one vendor's ladder, strongest first, as ids.
//
// These are the same strings the ranking matches on, so every id this returns
// is one ClampToCeiling can see — there exists no rung the ceiling cannot rank.
// A vendor kolk does not know has no ladder, and gets nothing rather than a
// guess.
func LadderRungIDs(vendor string) []string {
	for _, ladder := range vendorLadders {
		if ladder.name != vendor {
			continue
		}
		rungs := make([]string, len(ladder.rungs))
		copy(rungs, ladder.rungs)
		return rungs
	}
	return nil
}

// ModelsBelowCeiling is every rung on the ceiling's ladder that a run could
// climb down to, cheapest last. What it names is the ladder, not the roster:
// a rung is reachable only once its vendor is signed in, which is the surface's
// question, and this is what the surface uses to say that a sign-in would
// change something.
func ModelsBelowCeiling(ceiling string) []string {
	ladderName, rung, known := modelRank(ceiling)
	if !known {
		return nil
	}
	for _, candidate := range vendorLadders {
		if candidate.name != ladderName {
			continue
		}
		below := make([]string, 0, len(candidate.rungs)-rung-1)
		below = append(below, candidate.rungs[rung+1:]...)
		return below
	}
	return nil
}

// ModelsAboveCeiling is every model on the ceiling's ladder that this session
// may NOT use. It is what a session can honestly state: the ceiling refuses
// these, which is a guarantee, where naming the cheaper rungs would predict a
// routing decision that has not been made.
func ModelsAboveCeiling(ceiling string) []string {
	ladderName, rung, known := modelRank(ceiling)
	if !known || rung == 0 {
		return nil
	}
	for _, candidate := range vendorLadders {
		if candidate.name != ladderName {
			continue
		}
		blocked := make([]string, 0, rung)
		blocked = append(blocked, candidate.rungs[:rung]...)
		return blocked
	}
	return nil
}
