package engine

import (
	"context"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

const (
	defaultPaidFastLaneModel = "google/gemini-2.5-flash"
)

// FastLaneModel returns the model identifier chosen for fast, low-cost
// auxiliary engine operations (titling, compaction summaries, commit messages).
// If the active session model is free, FastLane uses the free model.
// If the session model is paid, FastLane uses a high-throughput, low-cost model.
func (a *Agent) FastLaneModel() string {
	if a.Model != "" && provider.ModelIsFree(provider.ModelInfo{ID: a.Model}) {
		return a.Model
	}
	if len(a.FreeModels) > 0 && provider.ModelIsFree(provider.ModelInfo{ID: a.FreeModels[0]}) && (a.Model == "" || provider.ModelIsFree(provider.ModelInfo{ID: a.Model})) {
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
	if err != nil {
		return "", err
	}
	if msg.Content != "" {
		return msg.Content, nil
	}
	return buf.String(), nil
}
