package engine

import (
	"context"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

const (
	defaultPaidFastLaneModel = "google/gemini-2.5-flash"
)

// FastLaneModel returns the model chosen for fast, low-cost auxiliary work:
// titling, compaction summaries, commit and pull-request drafts, and the
// boilerplate slot in an orchestrated run.
//
// **A free model wins whenever one is available**, whatever the session is
// running. This used to be the other way round — a paid session model sent the
// fast lane to a paid default even with a free tool-capable model discovered —
// which billed exactly the person who chose a strong model for real work every
// time a session was named. That was deliberate rather than accidental, and it
// is the decision item 33 changed: the strong model is for the work, and naming
// a session is not the work.
//
// The order is therefore: the session's own model when it is already free
// (switching costs the prompt cache for nothing), then the best discovered free
// model, then the low-cost paid default when no free model exists. `slot.fast`
// overrides all of it for anyone who disagrees.
func (a *Agent) FastLaneModel() string {
	if a.Model != "" && provider.ModelIsFree(provider.ModelInfo{ID: a.Model}) {
		return a.Model
	}
	if len(a.FreeModels) > 0 && provider.ModelIsFree(provider.ModelInfo{ID: a.FreeModels[0]}) {
		return a.FreeModels[0]
	}
	return defaultPaidFastLaneModel
}

// FastLaneChat performs an auxiliary, isolated chat completion with no tools
// and zero side-effects on session messages.
func (a *Agent) FastLaneChat(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	model := a.FastLaneModel()
	var messages []provider.Message
	if systemPrompt != "" {
		messages = append(messages, provider.Message{
			Role:    "system",
			Content: systemPrompt,
		})
	}
	messages = append(messages, provider.Message{
		Role:    "user",
		Content: userPrompt,
	})

	var buf strings.Builder
	onToken := func(tok string) {
		buf.WriteString(tok)
	}

	msg, _, err := a.Backend.StreamChat(ctx, model, messages, nil, onToken)
	if err != nil && model != defaultPaidFastLaneModel {
		// Free tiers rate-limit, and this path calls the backend directly
		// rather than through the turn's free-model rotation. Preferring free
		// without a net would trade money for a session title that sometimes
		// fails, so one fallback — and only one, because a fast lane that keeps
		// trying is a fast lane that stalls the thing it was helping.
		msg, _, err = a.Backend.StreamChat(ctx, defaultPaidFastLaneModel, messages, nil, onToken)
	}
	if err != nil {
		return "", err
	}
	if msg.Content != "" {
		return msg.Content, nil
	}
	return buf.String(), nil
}
