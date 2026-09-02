package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

var rateLimitRetryDelays = [...]time.Duration{
	time.Second,
	2 * time.Second,
	4 * time.Second,
}

const maxRateLimitRetryDelay = 4 * time.Second

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// streamChat is the engine's single pre-stream retry boundary. HTTPError can
// only be returned before a successful streaming response is handed to the
// scanner, so this never replays output already shown to the user.
func (a *Agent) streamChat(ctx context.Context, phase, model string, messages []provider.Message, toolset []provider.Tool, onToken func(string)) (provider.Message, provider.Meta, error) {
	return a.streamChatObserved(ctx, phase, model, messages, toolset, onToken, nil)
}

// streamChatObserved is streamChat with optional typed provider boundaries.
// Backends that only implement ChatBackend keep the original call path.
func (a *Agent) streamChatObserved(ctx context.Context, phase, model string, messages []provider.Message, toolset []provider.Tool, onToken func(string), observe func(provider.ProgressEvent)) (provider.Message, provider.Meta, error) {
	return a.streamChatOnObserved(ctx, pinnedBackend{}, phase, model, messages, toolset, onToken, true, observe)
}

// streamChatOnObserved retains streamChatOn's retry/routing behaviour while
// projecting meaningful provider-owned boundaries when a backend exposes them.
func (a *Agent) streamChatOnObserved(ctx context.Context, pinned pinnedBackend, phase, model string, messages []provider.Message, toolset []provider.Tool, onToken func(string), tokensVisible bool, observe func(provider.ProgressEvent)) (provider.Message, provider.Meta, error) {
	stopActivity := func() {}
	if a.Activity != nil {
		if stop := a.Activity.Start(ctx, phase); stop != nil {
			stopActivity = stop
		}
	}
	var stopOnce sync.Once
	stop := func() { stopOnce.Do(stopActivity) }
	defer stop()

	streamToken := onToken
	// A visible token replaces the spinner, so retire it immediately before
	// drawing that token for line-oriented renderers. A redraw-based surface can
	// keep the indicator in its owned status row while appending the token to a
	// different region, so it opts into the persistent path explicitly.
	keepActivity := false
	if persistent, ok := a.Activity.(PersistentActivityIndicator); ok {
		keepActivity = persistent.KeepActivityDuringOutput()
	}
	if onToken != nil && tokensVisible && !keepActivity {
		streamToken = func(token string) {
			stop()
			onToken(token)
		}
	}

	tried := map[string]bool{model: true}
	for retry := 0; ; retry++ {
		// Resolved every attempt, not once: rotation and the metered fallback
		// change `model` inside this loop, and the backend has to follow it.
		backend, wire, routeErr := a.backendFor(model)
		if routeErr != nil {
			return provider.Message{}, provider.Meta{Model: model}, routeErr
		}
		// Still the model this provider was opened for? Then use it. Once the
		// loop has moved on, the route is the only thing that knows where the
		// new model lives.
		if own := pinned.forModel(model); own != nil {
			backend = own
		}
		messageSeen := false
		observeAttempt := func(event provider.ProgressEvent) {
			// A provider can stream thousands of text deltas. The work ledger
			// records the transition into responding once per physical attempt;
			// tool, error, and limit boundaries remain individually observable.
			if event.Kind == provider.ProgressMessage {
				if messageSeen {
					return
				}
				messageSeen = true
			}
			if observe != nil {
				observe(event)
			}
		}
		var msg provider.Message
		var meta provider.Meta
		var err error
		if observed, ok := backend.(provider.ObservedChatBackend); ok && observe != nil {
			msg, meta, err = observed.StreamChatObserved(ctx, wire, messages, toolset, streamToken, observeAttempt)
		} else {
			msg, meta, err = backend.StreamChat(ctx, wire, messages, toolset, streamToken)
		}
		if err == nil {
			return msg, meta, nil
		}
		// The user's own Ollama is its own cost class (E7): no plan to bill
		// against, no free rotation to run, no backoff worth waiting. Checked
		// first, because a cloud limit's wording would otherwise read as an
		// exhausted plan two lines down.
		if httpErr, host := hostRefusal(err); host {
			return provider.Message{}, meta, explainHostRefusal(model, httpErr, err)
		}

		// An exhausted allowance is checked before the rate-limit gate below:
		// waiting never clears it, and two of its shapes — 402, and a vendor
		// CLI's prose — do not reach that gate at all.
		if subscriptionLimited(err) {
			next, ok := a.resolveSubscriptionLimit(ctx, model)
			if !ok {
				return provider.Message{}, meta, fmt.Errorf("%s is out of allowance and the run stopped; `/config set routing.on_subscription_limit switch` to continue on a metered model instead: %w", model, err)
			}
			a.moveToMetered(next)
			fmt.Fprintf(a.Out, "◆ subscription out of allowance; continuing on %s, billed per token\n", next)
			model = next
			tried[next] = true
			retry = -1
			continue
		}

		var httpErr *provider.HTTPError
		if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusTooManyRequests {
			return provider.Message{}, meta, err
		}

		// `stop` substitutes nothing: no rotation, no metered fallback. Someone
		// who set it would rather see this error than find out afterwards that
		// three models answered one question.
		onFree, freeErr := NormalizeFreeExhausted(a.OnFreeExhausted)
		if freeErr != nil {
			onFree = OnFreeExhaustedFree
		}
		if onFree == OnFreeExhaustedStop && provider.ModelIsFree(provider.ModelInfo{ID: model}) {
			return provider.Message{}, meta, fmt.Errorf("%s is rate-limited and routing.on_free_exhausted is `stop`, so nothing was substituted; set it to free or paid to keep going: %w", model, err)
		}

		if !a.PinnedModel && provider.ModelIsFree(provider.ModelInfo{ID: model}) && len(a.FreeModels) > 1 {
			var nextCandidate string
			for _, cand := range a.FreeModels {
				if !tried[cand] {
					nextCandidate = cand
					break
				}
			}
			if nextCandidate != "" {
				tried[nextCandidate] = true
				fmt.Fprintf(a.Out, "◆ free model rate-limited (429); rotating to %s\n", nextCandidate)
				model = nextCandidate
				a.SetSessionModel(nextCandidate)
				if a.Sess != nil {
					a.Sess.SetModelName(nextCandidate)
				}
				retry = -1
				continue
			}
		}

		if retry >= len(rateLimitRetryDelays) {
			// Only now is free genuinely exhausted: every free model has been
			// tried and the last one has had its bounded retries. Doing this
			// any earlier would skip the backoff that a transient rate limit
			// usually clears within — which is what rotation exists to give it.
			if provider.ModelIsFree(provider.ModelInfo{ID: model}) && allFreeModelsTried(a.FreeModels, tried) {
				if onFree == OnFreeExhaustedPaid {
					if metered := a.meteredFallback(model); metered != "" {
						fmt.Fprintf(a.Out, "◆ every free model is rate-limited; continuing on %s, billed per token\n", metered)
						a.moveToMetered(metered)
						model = metered
						tried[metered] = true
						retry = -1
						continue
					}
				}
				return provider.Message{}, meta, fmt.Errorf("every free model is rate-limited and routing.on_free_exhausted is `%s`; `/config set routing.on_free_exhausted paid` allows a metered fallback, or use `/model`: %w", onFree, err)
			}
			return provider.Message{}, meta, fmt.Errorf("model %s remains rate-limited after %d attempts; use `/model` to select another model: %w", model, retry+1, err)
		}

		delay := rateLimitRetryDelays[retry]
		if httpErr.RetryAfter > maxRateLimitRetryDelay {
			return provider.Message{}, meta, fmt.Errorf("model %s is rate-limited for at least %s; retry later or use `/model` to select another model: %w", model, httpErr.RetryAfter.Round(time.Second), err)
		}
		if httpErr.RetryAfter > delay {
			delay = httpErr.RetryAfter
		}
		if err := a.RetryWait(ctx, delay); err != nil {
			return provider.Message{}, meta, err
		}
	}
}

// allFreeModelsTried reports that this run has already asked every free model
// it knows about. An empty list is not "all tried": it means the run never had
// a free rotation to exhaust, and treating that as exhaustion would turn a
// single 429 on a free model into a billed one.
func allFreeModelsTried(free []string, tried map[string]bool) bool {
	if len(free) == 0 {
		return false
	}
	for _, model := range free {
		if !tried[model] {
			return false
		}
	}
	return true
}

// meteredFallback names the per-token model to continue on, refusing one that
// is the model that just failed.
func (a *Agent) meteredFallback(current string) string {
	if a.MeteredModel == nil {
		return ""
	}
	metered := strings.TrimSpace(a.MeteredModel())
	if metered == current {
		return ""
	}
	return metered
}
