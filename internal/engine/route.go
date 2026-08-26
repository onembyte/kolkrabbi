package engine

import (
	"fmt"
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

// modelForKind picks the model for one task.
//
// The fallback chain is deliberately short: an explicit slot, then the fast
// lane for mechanical work because that lane already exists and already knows
// how to pick something cheap, then the model the run is already using. A user
// who configured nothing gets exactly today's behaviour.
func (a *Agent) modelForKind(kind Kind) string {
	slot, routed := kindSlots[kind]
	if routed {
		if model := strings.TrimSpace(a.Slots[slot]); model != "" {
			return model
		}
		if slot == SlotFast {
			return a.FastLaneModel()
		}
	}
	return a.modelFor(a.Effort)
}
