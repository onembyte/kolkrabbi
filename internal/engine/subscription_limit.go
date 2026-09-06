package engine

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// The three answers to "your subscription is out of allowance".
//
// ask is the default, and deliberately so: switching to a metered model spends
// money, and a run that starts billing someone because a plan ran out is a
// surprise on a card statement rather than a decision anybody made.
const (
	OnLimitAsk    = "ask"
	OnLimitSwitch = "switch"
	OnLimitStop   = "stop"
)

// NormalizeSubscriptionLimit turns configured text into one of the three
// policies. Empty means unset, which is ask.
func NormalizeSubscriptionLimit(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return OnLimitAsk, nil
	case OnLimitAsk:
		return OnLimitAsk, nil
	case OnLimitSwitch:
		return OnLimitSwitch, nil
	case OnLimitStop:
		return OnLimitStop, nil
	default:
		return "", fmt.Errorf("unknown policy %q: use ask, switch or stop", strings.TrimSpace(value))
	}
}

// subscriptionLimited reports whether err is an allowance running out rather
// than a request arriving too fast. The two look alike over HTTP and want
// opposite responses: a rate limit is waited out, an exhausted plan never
// clears by waiting.
//
// Only two signals are structured — 402, and a gateway that labels the limit's
// source. A subscription answered by a vendor CLI returns prose, so that case
// is matched on wording and will miss phrasings nobody has seen yet. A miss
// costs nothing: the error surfaces exactly as it does today. A false positive
// would stop a healthy run, so the wording test stays narrow on purpose.
// subscriptionLimited is the continuity classifier's answer to one question:
// is this the plan or the account saying no (as opposed to the endpoint, the
// model, or the network)? Behaviour unchanged from before V35.1a; the
// classification now lives in one place, provider.Classify.
func subscriptionLimited(err error) bool {
	limit, ok := provider.Classify(err)
	if !ok {
		return false
	}
	return limit.Kind == provider.LimitSubscriptionAllowance || limit.Kind == provider.LimitAccountQuota
}

func (a *Agent) afterSubscriptionLimit(ctx context.Context, policy, metered string) (string, bool) {
	if strings.TrimSpace(metered) == "" {
		return "", false
	}
	switch policy {
	case OnLimitStop:
		return "", false
	case OnLimitSwitch:
		return metered, true
	}

	// ask. Nobody to ask is not a yes. A run left alone — `--full-auto`, a cron
	// tick, a pipe — must neither hang on a prompt no one will see nor decide
	// on its own to start spending; it stops and says why.
	if a.Ask == nil {
		return "", false
	}
	keepGoing := "Continue on " + metered
	answer, ok := a.Ask.Choose(ctx, Choice{
		Question: fmt.Sprintf("Your subscription is out of allowance. Continue on %s, which is billed per token?", metered),
		Options:  []string{keepGoing, "Stop here"},
	})
	if !ok || answer != keepGoing {
		return "", false
	}
	return metered, true
}

// resolveSubscriptionLimit settles, once for the whole session, what happens
// after a subscription runs out, and returns the model to continue on.
//
// current is the model that just hit the limit, so a fallback that is already
// the failing model is no fallback, and a second limit on the model we moved
// to ends the run instead of switching in a circle.
func (a *Agent) resolveSubscriptionLimit(ctx context.Context, current string) (string, bool) {
	a.limitMu.Lock()
	defer a.limitMu.Unlock()

	if a.limitDecided {
		if a.limitModel == "" || a.limitModel == current {
			return "", false
		}
		return a.limitModel, true
	}

	metered := ""
	if a.MeteredModel != nil {
		metered = strings.TrimSpace(a.MeteredModel())
	}
	if metered == current {
		metered = ""
	}
	policy, err := NormalizeSubscriptionLimit(a.OnSubscriptionLimit)
	if err != nil {
		// An unreadable policy is not permission to spend.
		policy = OnLimitAsk
	}
	next, ok := a.afterSubscriptionLimit(ctx, policy, metered)
	a.limitDecided = true
	if ok {
		a.limitModel = next
	}
	return next, ok
}

// moveToMetered points the session at a per-token model and at the gateway that
// bills it. The retired plan provider owns a child process, and nothing else
// will release it.
func (a *Agent) moveToMetered(model string) {
	// Read and swap under one lock, so a reader cannot see the old backend
	// paired with the new model. Closing happens after the lock is released:
	// the retired provider's Close waits on a child process, and holding a
	// session-wide lock across that would stall every other subagent.
	var retired ChatBackend
	a.modelMu.Lock()
	if a.Client != nil && a.Backend != ChatBackend(a.Client) {
		retired = a.Backend
		a.Backend = a.Client
	}
	a.Model = model
	a.modelMu.Unlock()

	if closer, ok := retired.(io.Closer); ok {
		_ = closer.Close()
	}
	if a.Sess != nil {
		a.Sess.SetModelName(model)
	}
}
