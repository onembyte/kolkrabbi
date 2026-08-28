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

// RankForSlot orders the catalogue by what a slot is for.
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
func RankForSlot(models []provider.ModelInfo, slot string) []string {
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
