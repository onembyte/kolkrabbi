package engine

// The roster is the closed set of models an orchestrated run may spend on.
//
// Rung 0 is the model the user selected, and every rung after it is cheaper.
// That ordering is the guarantee: a model above the user's choice is not
// something the roster rejects, it is something the roster cannot express. The
// clamp in ceiling.go stays as defence in depth for the paths that still take a
// model by name, but nothing on this path can produce one.

// Rung is one model a run may use, and how far below the ceiling it sits.
type Rung struct {
	// Model is what a backend is asked for. For rung 0 this is the session's
	// own model, verbatim — never a ladder string, because a ladder rung is a
	// match PREFIX for ranking and handing one to a provider as an id would ask
	// for a model nobody offers.
	Model string
	// Vendor is the ladder this rung belongs to, empty when the session model
	// is on no ladder kolk knows.
	Vendor string
	// Depth is 0 for the ceiling and larger for cheaper models.
	Depth int
}

// Roster is the run's whole menu, strongest first.
type Roster struct {
	Rungs []Rung
}

// Cheapest is the rung a trivial task should reach for.
func (r Roster) Cheapest() Rung {
	if len(r.Rungs) == 0 {
		return Rung{}
	}
	return r.Rungs[len(r.Rungs)-1]
}

// Ceiling is the model the user selected.
func (r Roster) Ceiling() Rung {
	if len(r.Rungs) == 0 {
		return Rung{}
	}
	return r.Rungs[0]
}

// RungAvailable answers whether a vendor can actually run one of its rungs.
//
// It is a function rather than a method because the answer lives outside the
// engine: it depends on which connectors are signed in, which is state the
// surface owns. The engine may not import the adapter layer, so the capability
// arrives as this.
type RungAvailable func(vendor, model string) bool

// roster builds the menu for this session.
//
// The ceiling is never checked for availability — the session is already
// running on it, so asking whether it can run is a question that has already
// been answered by the session existing. Everything below it is asked about,
// and absent unless the answer is yes: a menu that offers what cannot be
// spawned is a menu of errors, and it would fail at the user's first task
// rather than here.
// Roster is the exported form, for a surface that wants to show the lane.
func (a *Agent) Roster(available RungAvailable) Roster { return a.roster(available) }

func (a *Agent) roster(available RungAvailable) Roster {
	model := a.SessionModel()
	vendor, depth, ranked := modelRank(model)
	roster := Roster{Rungs: []Rung{{Model: model, Vendor: vendor, Depth: 0}}}
	if !ranked {
		// Nothing to climb down. Everything runs on the user's model, which is
		// exactly what happens today on a gateway session.
		return roster
	}
	if available == nil {
		return roster
	}
	for _, id := range LadderRungIDs(vendor)[depth+1:] {
		if !available(vendor, id) {
			continue
		}
		roster.Rungs = append(roster.Rungs, Rung{
			Model:  id,
			Vendor: vendor,
			Depth:  len(roster.Rungs),
		})
	}
	return roster
}
