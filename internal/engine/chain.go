package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

// ErrNothingToContinue is /continue with no equivalent left to try, or with no
// pause to continue from.
var ErrNothingToContinue = errors.New("nothing configured can continue this on an equal rung; the pause stands and kolk resumes at the reset (/plans and /key add options)")

// ContinueOn walks the chain from the given position (plan 35 §2.5, V35.4a):
// the recommendation's equivalents in order, each switched to through the
// surface, a hop that fails set aside for the next, until one takes. The
// pause is then lifted and the turn that was waiting is returned for the
// surface to run on the new model. Every hop is printed and published as a
// switch; nothing about it is persisted. The cooldown registry is reloaded
// first so a limit another session met since is respected at this hop.
func (a *Agent) ContinueOn(ctx context.Context, from int) (string, continuity.Candidate, error) {
	if a.Sess == nil {
		return "", continuity.Candidate{}, ErrNothingToContinue
	}
	pause := a.Sess.Paused()
	if pause == nil {
		return "", continuity.Candidate{}, ErrNothingToContinue
	}
	if a.Switch == nil {
		return "", continuity.Candidate{}, errors.New("this surface cannot switch models mid-session; /model does it by hand")
	}
	if a.Cooldowns != nil {
		_ = a.Cooldowns.Reload()
	}
	limit := pause.Limit()
	chain := a.chain(limit)
	if from < 0 {
		from = 0
	}
	if from >= len(chain) {
		return "", continuity.Candidate{}, ErrNothingToContinue
	}
	for _, candidate := range chain[from:] {
		label, err := a.Switch(ctx, candidate)
		if err != nil {
			fmt.Fprintf(a.Out, "◆ %s could not take over: %v\n", candidate.Ref(), err)
			continue
		}
		a.stopResumeMonitor()
		a.Sess.SetPaused(nil)
		a.save()
		fmt.Fprintf(a.Out, "◆ %s/%s %s; continuing on %s at %s (%s)\n",
			limit.Connector, limit.Model, pause.HumanKind(), label, a.Effort, billingWordFor(candidate))
		a.publishLimit(provider.Limit{Kind: limit.Kind, Scope: limit.Scope, Model: candidate.Model, Connector: candidate.Connector, Source: "chain"}, "switch")
		return pause.PendingTurn, candidate, nil
	}
	return "", continuity.Candidate{}, ErrNothingToContinue
}

func billingWordFor(c continuity.Candidate) string {
	switch {
	case c.Free:
		return "free"
	case c.Billing == provider.BillingSubscription:
		return "subscription"
	case c.Billing == provider.BillingAPIMetered:
		return "API key, metered"
	case c.Billing == provider.BillingGateway:
		return "gateway, per token"
	}
	return c.Billing
}
