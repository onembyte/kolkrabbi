package agentcli

import (
	"time"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// Collect projects translated Claude events into the provider-neutral result
// shape used by engine adapters. It never reconstructs or stores raw frames.
func Collect(events []Event, elapsed time.Duration) (provider.Message, provider.Meta, error) {
	message, meta, err := collect(events, elapsed)
	// A handover's reply is the plan's turn, whatever the vendor's frame says
	// about cost: kolk never turns a subscription turn into a dollar figure.
	meta.Billing = provider.BillingSubscription
	return message, meta, err
}

func collect(events []Event, elapsed time.Duration) (provider.Message, provider.Meta, error) {
	var message provider.Message
	var meta provider.Meta
	for _, event := range events {
		switch event.Kind {
		case EventInit:
			// An init that only carries a session handle (Copilot's terminal
			// result) must not blank a model an earlier event named.
			if event.Model != "" {
				meta.Model = event.Model
			}
		case EventTool:
			// One tool_use is one run; the later tool_result merely completes
			// the one already counted.
			if event.ToolName != "" {
				meta.ToolCalls++
			}
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
