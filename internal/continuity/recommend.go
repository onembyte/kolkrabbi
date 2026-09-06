package continuity

import (
	"fmt"
	"sort"
	"strings"
)

// Candidate is one way the work could continue: a model behind a connector
// or a key, with what is known about how it is billed, how the person has
// rated it, and what it can do. The list is the surface's to assemble — it
// knows the connectors, the keys and the catalog — and this package's to
// rank (plan 35 §2.3, V35.3a).
type Candidate struct {
	Model     string
	Exact     string // the model a routing word like copilot's `auto` resolved to
	Connector string
	Plan      string
	Billing   string // provider.Billing*: subscription | api-metered | gateway | local
	Free      bool
	Preferred bool // on the person's continuity.preferred list
	Rating    float64
	Ratings   int
	// LacksTools is a known lack; Context is a known window, zero unknown.
	LacksTools bool
	Context    int
}

// Need is what the interrupted task requires of a continuation.
type Need struct {
	Tools   bool
	Context int
}

// Rank is the vendor ladder as the engine keeps it: which ladder a model
// sits on and its rung from the top (0 is the top), or unknown.
type Rank func(model string) (ladder string, rung int, ok bool)

// Cooling reports whether a connector or model is cooling after a limit.
type Cooling func(connector, model string) bool

// Exclusion is a candidate that could not be recommended and the reason,
// said so the block can show it rather than hide it.
type Exclusion struct {
	Candidate Candidate
	Why       string
}

// Recommendation is the answer: the equivalents in order, the best first,
// what was set aside and why, and a note when nothing could be compared.
type Recommendation struct {
	Current    Candidate
	Top        *Candidate
	Equivalent []Candidate
	Excluded   []Exclusion
	Note       string
}

// DefaultOrder is the owner's: every subscription first, then paid, then free.
var DefaultOrder = []string{"subscription", "paid", "free"}

// Recommend ranks the candidates that could continue the work the current
// model stopped. Equivalence is by rung: the same, one above, or one below
// — never further below, and never a free model unless it is preferred, so
// a run on a frontier model is not quietly continued on a 7B (owner answer
// 2). Within the owner's order, the person's own ratings decide.
func Recommend(current Candidate, need Need, candidates []Candidate, order []string, cooling Cooling, rank Rank) Recommendation {
	rec := Recommendation{Current: current}
	if len(order) == 0 {
		order = DefaultOrder
	}
	curLadder, curRung, curKnown := rankOf(rank, current)
	if !curKnown {
		rec.Note = fmt.Sprintf("%s sits on no rung kolk knows, so only a preferred model counts as its equal", current.Model)
	}
	for _, c := range candidates {
		if why := ineligible(current, c, need, cooling); why != "" {
			rec.Excluded = append(rec.Excluded, Exclusion{Candidate: c, Why: why})
			continue
		}
		if c.Preferred {
			rec.Equivalent = append(rec.Equivalent, c)
			continue
		}
		if c.Free {
			rec.Excluded = append(rec.Excluded, Exclusion{Candidate: c, Why: "free: never a subscription's equal unless preferred"})
			continue
		}
		if !curKnown {
			rec.Excluded = append(rec.Excluded, Exclusion{Candidate: c, Why: "no rung to compare against"})
			continue
		}
		_, rung, ok := rankOf(rank, c)
		if !ok {
			rec.Excluded = append(rec.Excluded, Exclusion{Candidate: c, Why: "no rung known for it"})
			continue
		}
		_ = curLadder
		switch delta := rung - curRung; {
		case delta > 1:
			rec.Excluded = append(rec.Excluded, Exclusion{Candidate: c, Why: fmt.Sprintf("%d rungs below", delta)})
		case delta < -1:
			rec.Excluded = append(rec.Excluded, Exclusion{Candidate: c, Why: fmt.Sprintf("%d rungs above", -delta)})
		default:
			rec.Equivalent = append(rec.Equivalent, c)
		}
	}
	groupIndex := func(c Candidate) int {
		g := billingGroup(c)
		for i, name := range order {
			if name == g {
				return i
			}
		}
		return len(order)
	}
	sort.SliceStable(rec.Equivalent, func(i, j int) bool {
		a, b := rec.Equivalent[i], rec.Equivalent[j]
		if ga, gb := groupIndex(a), groupIndex(b); ga != gb {
			return ga < gb
		}
		if a.Rating != b.Rating {
			return a.Rating > b.Rating
		}
		return a.Ratings > b.Ratings
	})
	if len(rec.Equivalent) > 0 {
		top := rec.Equivalent[0]
		rec.Top = &top
	}
	return rec
}

func ineligible(current, c Candidate, need Need, cooling Cooling) string {
	switch {
	case c.Connector == current.Connector && c.Model == current.Model:
		return "the one that stopped"
	case cooling != nil && cooling(c.Connector, c.Model):
		return "cooling after a limit"
	case need.Tools && c.LacksTools:
		return "cannot run tools"
	case need.Context > 0 && c.Context > 0 && c.Context < need.Context:
		return fmt.Sprintf("context %d below the %d the task needs", c.Context, need.Context)
	}
	return ""
}

func rankOf(rank Rank, c Candidate) (string, int, bool) {
	if rank == nil {
		return "", 0, false
	}
	if c.Exact != "" {
		if ladder, rung, ok := rank(c.Exact); ok {
			return ladder, rung, true
		}
	}
	return rank(c.Model)
}

// billingGroup folds the billing modes into the owner's three groups.
func billingGroup(c Candidate) string {
	switch {
	case c.Free:
		return "free"
	case strings.EqualFold(c.Billing, "subscription"):
		return "subscription"
	}
	return "paid"
}

// Label is how a candidate is named to a person: the model, its plan when it
// has one, its billing path, and the person's rating when there is one.
func (c Candidate) Label() string {
	b := &strings.Builder{}
	b.WriteString(c.Model)
	if c.Plan != "" {
		b.WriteString(" on " + c.Plan)
	}
	b.WriteString(" (" + billingWord(c))
	if c.Ratings > 0 {
		fmt.Fprintf(b, ", rated %.1f★ ×%d", c.Rating, c.Ratings)
	}
	b.WriteString(")")
	return b.String()
}

// Command is what applies a candidate: the plan-qualified model reference
// for a handover, the bare id for a keyed or gateway model.
func (c Candidate) Command() string {
	if c.Plan != "" {
		return fmt.Sprintf("/model %q", c.Plan+"/"+c.Model)
	}
	return "/model " + c.Model
}

func billingWord(c Candidate) string {
	switch {
	case c.Free:
		return "free"
	case strings.EqualFold(c.Billing, "subscription"):
		return "subscription"
	case strings.EqualFold(c.Billing, "gateway"):
		return "gateway, per token"
	case strings.EqualFold(c.Billing, "api-metered"):
		return "API key, metered"
	case strings.EqualFold(c.Billing, "local"):
		return "local"
	}
	return c.Billing
}

// Lines is the block a surface prints after the pause line: the best
// equivalent with the command that applies it, the others in order, and what
// was set aside with the reason. Nothing here is applied.
func (r Recommendation) Lines() []string {
	var lines []string
	if r.Note != "" {
		lines = append(lines, r.Note)
	}
	if r.Top == nil {
		lines = append(lines, "nothing else configured could continue this on an equal rung; /plans and /key add options")
	} else {
		lines = append(lines, "Equivalent now: "+r.Top.Label()+" — "+r.Top.Command())
		for _, c := range r.Equivalent[1:] {
			lines = append(lines, "  also: "+c.Label()+" — "+c.Command())
		}
	}
	if len(r.Excluded) > 0 {
		parts := make([]string, 0, len(r.Excluded))
		for _, x := range r.Excluded {
			parts = append(parts, x.Candidate.Model+" ("+x.Why+")")
		}
		lines = append(lines, "Set aside: "+strings.Join(parts, "; "))
	}
	return lines
}

// Ref is the reference a surface switches to: plan-qualified for a handover,
// the bare id otherwise.
func (c Candidate) Ref() string {
	if c.Plan != "" {
		return c.Plan + "/" + c.Model
	}
	return c.Model
}

// PreferredChain is the chain when the person chose their own list (plan 35
// §2.4 `select preferred`): exactly that list, in that order, keeping only
// candidates that are eligible now; equivalence is not enforced, because
// they wrote the list. A name may be plan-qualified or bare.
func PreferredChain(current Candidate, need Need, candidates []Candidate, preferred []string, cooling Cooling) []Candidate {
	var chain []Candidate
	for _, name := range preferred {
		for _, c := range candidates {
			if !strings.EqualFold(name, c.Ref()) && !strings.EqualFold(name, c.Model) {
				continue
			}
			if ineligible(current, c, need, cooling) != "" {
				continue
			}
			chain = append(chain, c)
			break
		}
	}
	return chain
}
