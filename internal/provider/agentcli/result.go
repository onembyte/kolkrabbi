package agentcli

import (
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// Collect projects translated Claude events into the provider-neutral result
// shape used by engine adapters. It never reconstructs or stores raw frames.
func Collect(events []Event, elapsed time.Duration) (provider.Message, provider.Meta, error) {
	var message provider.Message
	var meta provider.Meta
	for _, event := range events {
		switch event.Kind {
		case EventInit:
			meta.Model = event.Model
		case EventMessageDelta:
			message.Role = "assistant"
			message.Content += event.Text
			if event.Model != "" {
				meta.Model = event.Model
			}
		case EventMessageCompleted:
			message.Role = "assistant"
			message.Content = event.Text
			if event.Model != "" {
				meta.Model = event.Model
			}
		case EventUsage:
			if event.Model != "" {
				meta.Model = event.Model
			}
			meta.PromptTokens = event.InputTokens
			meta.CompletionTokens = event.OutputTokens
			meta.CacheReadTokens = event.CacheRead
			meta.CacheCreationTokens = event.CacheCreation
			meta.Cost = event.CostUSD
		case EventError:
			return provider.Message{}, meta, &providerError{message: event.Error}
		}
	}
	meta.Elapsed = elapsed
	return message, meta, nil
}

type providerError struct{ message string }

func (e *providerError) Error() string { return e.message }
