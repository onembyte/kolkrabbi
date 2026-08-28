package engine

import (
	"sort"
	"strconv"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// minSlotContext is the smallest window a subagent can work in.
//
// The same floor RankFreeModels already uses. A model below it cannot hold a
// task, its dependencies' results and a file, which is the minimum an
// orchestrated task carries.
const minSlotContext = 32768

// ModelRating is what this machine thinks of a model, from its own ratings.
//
// The one ranking signal no vendor benchmark has: not whether a model is good,
// but whether it was good at this person's work. Count travels with Average
// because one bad turn is a bad turn and not a verdict.
type ModelRating struct {
	Average float64
	Count   int
}

const (
	// minRatingsToCount is how much evidence moves a ranking.
	//
	// One rating must not rearrange a run: somebody who mis-clicks once should
	// not lose a model, and a single turn says more about the task than the
	// model. Three is enough to be a pattern and few enough to be reachable.
	minRatingsToCount = 3
	// A rating only counts when it is an opinion. A middling average is not a
	// signal, and treating it as one turns the ranking into noise with a
	// numeric face.
	ratingLiked    = 4.0
	ratingDisliked = 2.0
)

// ratingVerdict is -1, 0 or +1: disliked, no opinion, liked.
//
// Deliberately three values rather than a continuous weight. A weight invites
// tuning, and there is nothing here to tune against — this is one person's
// handful of ratings, not a benchmark.
func (a *Agent) ratingVerdict(model string) int {
	rating, rated := a.ModelRatings[model]
	if !rated || rating.Count < minRatingsToCount {
		return 0
	}
	switch {
	case rating.Average <= ratingDisliked:
		return -1
	case rating.Average >= ratingLiked:
		return 1
	default:
		return 0
	}
}

// rankForSlot orders the catalogue by what a slot is for.
//
// The slots exist and are configurable; until now nothing filled them, so an
// unset slot fell back to the effort model and **every subagent in a run used
// the same model as the main one**. The four roles want genuinely different
// things, and choosing per role is the whole of item 33's third ask.
//
// Cross-provider needs no plumbing: the gateway routes any model id, so a run
// whose orchestrator is one vendor's model and whose workers are another's is a
// matter of choosing. This is the choosing.
//
// Ranking reuses what already exists — `CodingSuitability`, tool support,
// `ModelIsFree` — rather than growing a second opinion about models beside the
// one `RankFreeModels` already holds.
// A nil verdict means no opinion, which is the state of a machine that has
// rated nothing — the normal one.
func rankForSlot(models []provider.ModelInfo, slot string, verdict func(provider.ModelInfo) int) []string {
	if verdict == nil {
		verdict = func(provider.ModelInfo) int { return 0 }
	}
	usable := make([]provider.ModelInfo, 0, len(models))
	for _, m := range models {
		// Every slot runs a subagent, and a subagent that cannot call a tool
		// cannot do the work any of these slots exist for.
		if m.ContextLength < minSlotContext || !provider.SupportsTools(m) {
			continue
		}
		usable = append(usable, m)
	}
	if len(usable) == 0 {
		return nil
	}

	var better func(a, b provider.ModelInfo) bool
	switch slot {
	case SlotOrchestrator:
		// Planning is where a weak model costs the most: the whole run is shaped
		// by it. Strongest first, price ignored.
		better = func(a, b provider.ModelInfo) bool {
			if sa, sb := provider.CodingSuitability(a), provider.CodingSuitability(b); sa != sb {
				return sa > sb
			}
			return a.ContextLength > b.ContextLength
		}
	case SlotWorker:
		// The work the user is watching. Coding-oriented, and free breaks a tie
		// because a free model that is equally suited is strictly better.
		better = func(a, b provider.ModelInfo) bool {
			if sa, sb := provider.CodingSuitability(a), provider.CodingSuitability(b); sa != sb {
				return sa > sb
			}
			if fa, fb := provider.ModelIsFree(a), provider.ModelIsFree(b); fa != fb {
				return fa
			}
			return a.ContextLength > b.ContextLength
		}
	case SlotExplore:
		// Reading and summarising: this slot's job is volume, so cheap and
		// high-context beats clever.
		//
		// Free first, because a free model that clears the context floor is
		// strictly better than a nearly-free one for work that is measured in
		// pages read. Among the rest, cheapest wins and a bigger window breaks
		// the tie — which is where a million-token model earns its place, when
		// nothing free is available.
		better = func(a, b provider.ModelInfo) bool {
			if fa, fb := provider.ModelIsFree(a), provider.ModelIsFree(b); fa != fb {
				return fa
			}
			if pa, pb := promptPrice(a), promptPrice(b); pa != pb {
				return pa < pb
			}
			return a.ContextLength > b.ContextLength
		}
	case SlotFast:
		// Mechanical work is free work when free exists, which is A33.3's
		// decision applied to the slot that does it.
		free := provider.RankFreeModels(models)
		if len(free) > 0 {
			return free
		}
		better = func(a, b provider.ModelInfo) bool { return promptPrice(a) < promptPrice(b) }
	default:
		// A slot nobody defined is not a licence to guess.
		return nil
	}

	sort.SliceStable(usable, func(i, j int) bool {
		// What this machine has actually seen outranks what a description
		// claims: a model rated badly for this person's work goes last whatever
		// its name suggests, and one rated well comes first. Demotion is not
		// exclusion — a badly rated model is still better than no model, so it
		// falls to the end of the list rather than off it.
		if vi, vj := verdict(usable[i]), verdict(usable[j]); vi != vj {
			return vi > vj
		}
		if better(usable[i], usable[j]) {
			return true
		}
		if better(usable[j], usable[i]) {
			return false
		}
		// The same catalogue must rank the same way twice, or a run stops being
		// reproducible for the person watching it.
		return usable[i].ID < usable[j].ID
	})

	ranked := make([]string, len(usable))
	for i, m := range usable {
		ranked[i] = m.ID
	}
	return ranked
}

// promptPrice is what a model charges per prompt token, or a large number when
// it will not say — an unpriced model is not a cheap model.
func promptPrice(m provider.ModelInfo) float64 {
	price, err := strconv.ParseFloat(m.Pricing.Prompt, 64)
	if err != nil {
		return 1
	}
	return price
}
