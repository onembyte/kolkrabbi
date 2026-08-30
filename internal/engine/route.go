package engine

import (
	"fmt"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"sort"
	"strings"
)

// Slots are named roles a model can fill in an orchestrated run.
//
// Two levels — kind to slot, slot to model — because collapsing them is what
// makes routing tables unmaintainable. A user thinks "reading should be cheap",
// not "research and explain should both be gemini-flash"; the slot is where
// that thought goes, and the kinds behind it can change without the config
// changing.
const (
	// SlotOrchestrator is planning-shaped work: the strongest model available.
	SlotOrchestrator = "orchestrator"
	// SlotWorker does the editing and testing the user is watching.
	SlotWorker = "worker"
	// SlotExplore reads and summarises. Cheap and high-context.
	SlotExplore = "explore"
	// SlotFast is mechanical work. The existing fast lane.
	SlotFast = "fast"
)

// kindSlots routes each kind of task to a slot.
var kindSlots = map[Kind]string{
	KindResearch:    SlotExplore,
	KindExplain:     SlotExplore,
	KindEdit:        SlotWorker,
	KindTest:        SlotWorker,
	KindDesign:      SlotOrchestrator,
	KindBoilerplate: SlotFast,
}

// knownSlots is the closed set, in the order a person should read them.
var knownSlots = []string{SlotOrchestrator, SlotWorker, SlotExplore, SlotFast}

// ValidateSlots rejects a slot name that is not one of the four.
//
// Silently ignoring `explorer` when the slot is `explore` means someone pays
// for the wrong model for as long as it takes them to notice, which on a
// setting nobody looks at twice is indefinitely.
func ValidateSlots(slots map[string]string) error {
	unknown := make([]string, 0, len(slots))
	for name := range slots {
		if !isKnownSlot(name) {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return fmt.Errorf("unknown model slot %s (the slots are %s)",
		strings.Join(unknown, ", "), strings.Join(knownSlots, ", "))
}

func isKnownSlot(name string) bool {
	for _, known := range knownSlots {
		if known == name {
			return true
		}
	}
	return false
}

// orchestrationModel is what the planner and the synthesis run on.
//
// They are the orchestrator's own calls, so they take the orchestrator slot. A
// slot with that name that reached only tasks the planner happened to label
// "design" would be a name meaning something other than what it says.
//
// There is no default: unset, these run on the session model, which is what
// they have always done. The planner is the one call that most determines a
// run's quality and it is a single call, so paying for a stronger model there
// is cheap — but that is a judgement for the person paying, not one to make on
// their behalf the first time they open the config.
func (a *Agent) orchestrationModel() string {
	if model := strings.TrimSpace(a.Slots[SlotOrchestrator]); model != "" {
		return a.underCeiling(model)
	}
	return a.underCeiling(a.modelFor(a.Effort))
}

// underCeiling holds every routing decision to the model the user selected.
//
// It is applied here, at the single point every subagent's model passes
// through, rather than at each of the branches above — a ceiling with an
// exception is not a ceiling, and the next branch someone adds would not know
// it was supposed to ask.
//
// A configured slot is normally the user's decision and beats every ranking,
// but not this: they configured the slot once and selected the model just now,
// and the nearer choice is the one that meant it.
func (a *Agent) underCeiling(model string) string {
	return ClampToCeiling(model, a.Model)
}

// modelForKind picks the model for one task.
//
// The fallback chain is deliberately short: an explicit slot, then the fast
// lane for mechanical work because that lane already exists and already knows
// how to pick something cheap, then the model the run is already using. A user
// who configured nothing gets exactly today's behaviour.
func (a *Agent) modelForKind(kind Kind) string {
	return a.underCeiling(a.routeKind(kind))
}

// routeKind is the choice; modelForKind is the choice held to the ceiling.
func (a *Agent) routeKind(kind Kind) string {
	slot, routed := kindSlots[kind]
	if routed {
		// A configured slot is the user's decision and beats every ranking.
		if model := strings.TrimSpace(a.Slots[slot]); model != "" {
			return model
		}
		if slot == SlotFast {
			return a.FastLaneModel()
		}
		// An unset slot used to fall straight through to the effort model,
		// which meant every subagent in a run used the same model as the main
		// one and the four roles existed on paper only. Choosing per role is
		// item 33's third ask, and the catalogue is what it chooses from.
		if chosen := a.slotModel(slot); chosen != "" {
			return chosen
		}
	}
	return a.modelFor(a.Effort)
}

// slotModel resolves one slot from the catalogue, remembering the answer.
//
// Selection runs once per slot per session rather than once per task: a plan
// with eight tasks must not rank the catalogue eight times, and a run whose
// worker model changed halfway through would be one nobody could reason about.
func (a *Agent) slotModel(slot string) string {
	if len(a.Catalog) == 0 {
		return ""
	}
	a.slotMu.Lock()
	defer a.slotMu.Unlock()
	if model, chosen := a.slotChoice[slot]; chosen {
		return model
	}
	var model string
	ranked := rankForSlot(a.Catalog, slot, func(m provider.ModelInfo) int {
		return a.ratingVerdict(m.ID)
	})
	if len(ranked) > 0 {
		model = ranked[0]
	}
	if a.slotChoice == nil {
		a.slotChoice = map[string]string{}
	}
	a.slotChoice[slot] = model
	return model
}
