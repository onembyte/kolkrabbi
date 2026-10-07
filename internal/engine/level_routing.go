package engine

import (
	"fmt"
	"slices"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// assignModels binds each task to the model that will run it.
//
// All of it happens here, on the goroutine that owns the plan, before any task
// starts. That is not a stylistic choice: `tasks[i].Model` is read by every
// subagent goroutine once the run begins, and writing it from one of them —
// which a mid-run re-resolution would do — is a data race on a slice the
// scheduler is also reading.
//
// The roster is resolved ONCE for the whole plan. Availability reads the
// connector manifest, so a plan of eight tasks must not read it eight times,
// and two tasks must not disagree about what was signed in because a login
// landed halfway through.
func (a *Agent) assignModels(tasks []Task) {
	roster := a.roster(a.RungAvailable)
	// A slot outside the menu is replaced by each task's level rung, so its
	// tasks may land on several models: one note per slot names them all.
	var outside []string
	usedFor := map[string][]string{}
	for i := range tasks {
		a.resolveTask(&tasks[i], roster)
		configured := strings.TrimSpace(a.Slots[kindSlots[tasks[i].Kind]])
		if !roster.Discovered || configured == "" || strings.EqualFold(configured, tasks[i].Model) {
			continue
		}
		if _, seen := usedFor[configured]; !seen {
			outside = append(outside, configured)
		}
		if !slices.Contains(usedFor[configured], tasks[i].Model) {
			usedFor[configured] = append(usedFor[configured], tasks[i].Model)
		}
	}
	if a.Out == nil {
		return
	}
	for _, configured := range outside {
		fmt.Fprintf(a.Out, "  routing: %s is outside the selected model's ranked menu; using %s\n", configured, strings.Join(usedFor[configured], ", "))
	}
}

func (a *Agent) resolveTask(task *Task, roster Roster) {
	task.Model = a.modelForTask(*task, roster)
	task.Vendor = ""
	for _, rung := range roster.Rungs {
		if rung.Model == task.Model {
			task.Vendor = rung.Vendor
			break
		}
	}
	wanted := effortForTask(task.Level, a.Effort)
	task.Effort = roster.effort(task.Model, wanted)
	task.CeilingEffort = roster.effort(roster.Ceiling().Model, wanted)
}

// modelForTask is the whole routing decision for one task, in order.
func (a *Agent) modelForTask(task Task, roster Roster) string {
	// A slot the user configured is their own decision about their own money,
	// and it beats what the planner judged — but not the ceiling, which they
	// chose more recently. underCeiling has the last word either way. A slot
	// outside a discovered menu names nothing this run can vouch for, so the
	// task's level routes it: for a cheap slot, the cheap work it was set
	// for, not the ceiling's price.
	if slot, routed := kindSlots[task.Kind]; routed {
		if model := strings.TrimSpace(a.Slots[slot]); model != "" {
			if chosen, vouched := roster.slot(model); vouched {
				return chosen
			}
		}
	}
	if model, bound := bindLevel(task.Level, roster); bound {
		return model
	}
	// No ladder, or nothing cheaper available: exactly the routing a gateway
	// session has always had.
	return a.modelForKind(task.Kind)
}

// bindLevel turns what the planner judged into a rung.
//
// Trivial work uses the lowest model; routine work uses the nearest lower
// model. Hard and unstated work retain the selected ceiling. A one-model menu
// stays useful at every level.
func bindLevel(level Level, roster Roster) (string, bool) {
	if len(roster.Rungs) == 0 {
		return "", false
	}
	if level == LevelRoutine && len(roster.Rungs) > 1 {
		return roster.Rungs[1].Model, true
	}
	if level == LevelTrivial {
		cheapest := roster.Cheapest()
		// Only when there is somewhere cheaper to go. With nothing signed in
		// below the ceiling, the cheapest rung IS the ceiling, and saying so
		// through this path rather than the fallback keeps the two agreeing.
		if cheapest.Depth > 0 {
			return cheapest.Model, true
		}
	}
	return roster.Ceiling().Model, true
}

func (r Roster) effort(model, wanted string) string {
	for _, rung := range r.Rungs {
		if rung.Model == model {
			effort, _ := provider.EffortForPlan(wanted, rung.Efforts)
			return effort
		}
	}
	return wanted
}

func (a *Agent) taskEffort(task Task) string {
	if task.Effort != "" {
		return task.Effort
	}
	return effortForTask(task.Level, a.Effort)
}
