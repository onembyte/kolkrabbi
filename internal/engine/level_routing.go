package engine

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
	for i := range tasks {
		tasks[i].Model = a.modelForTask(tasks[i], roster)
	}
}

// modelForTask is the whole routing decision for one task, in order.
func (a *Agent) modelForTask(task Task, roster Roster) string {
	// A slot the user configured is their own decision about their own money,
	// and it beats what the planner judged — but not the ceiling, which they
	// chose more recently. underCeiling has the last word either way.
	if slot, routed := kindSlots[task.Kind]; routed {
		if model := a.Slots[slot]; model != "" {
			return a.underCeiling(model)
		}
	}
	if model, bound := bindLevel(task.Level, roster); bound {
		return a.underCeiling(model)
	}
	// No ladder, or nothing cheaper available: exactly the routing a gateway
	// session has always had.
	return a.modelForKind(task.Kind)
}

// bindLevel turns what the planner judged into a rung.
//
// Only `trivial` descends. That is deliberate and worth stating: the user chose
// their model for their work, and quietly running ordinary work on something
// weaker would be a quality decision nobody asked for — the mirror image of the
// spending decision the ceiling refuses. Mechanical work is the one case where
// cheaper is not a compromise, and it is the case that makes a subscription
// last the day.
//
// `hard` and `routine` therefore both mean the ceiling, and so does an unstated
// level: a task whose difficulty could not be read is not one to hand to the
// cheapest thing available. Anyone who disagrees has `slot.*`, which is checked
// first and is exactly the place for "I want ordinary work cheaper too".
func bindLevel(level Level, roster Roster) (string, bool) {
	if len(roster.Rungs) == 0 {
		return "", false
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
