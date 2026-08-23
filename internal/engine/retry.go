package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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
func (a *Agent) streamChat(ctx context.Context, model string, messages []provider.Message, toolset []provider.Tool, onToken func(string)) (provider.Message, provider.Meta, error) {
	for retry := 0; ; retry++ {
		msg, meta, err := a.Client.StreamChat(ctx, model, messages, toolset, onToken)
		if err == nil {
			return msg, meta, nil
		}
		var httpErr *provider.HTTPError
		if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusTooManyRequests {
			return provider.Message{}, meta, err
		}
		if retry >= len(rateLimitRetryDelays) {
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
