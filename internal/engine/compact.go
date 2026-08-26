package engine

import (
	"fmt"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// Compaction stages, in the order they are spent. Each one gives up more
// meaning than the last, so compaction stops at the first that fits.
const (
	StageNone        = "none"
	StageToolResults = "tool results"
	StageToolCalls   = "tool calls"
	StageSummary     = "summary"
)

// Compaction is what one compaction gave up, so the caller can say so out loud.
type Compaction struct {
	Messages    []provider.Message
	Stage       string
	Replaced    int
	FreedTokens int
}

// Summarizer turns a span of history into one paragraph. It is injected
// because the summary comes from a model call, and the transform itself must
// stay testable without one.
type Summarizer func(messages []provider.Message) (string, error)

// CompactMessages shrinks a conversation toward targetTokens, sacrificing the
// least meaningful content first and stopping as soon as it fits.
//
// The system prompt and the most recent keepTurns turns are never touched: the
// recent turns are what the model needs most, and re-deriving the system prompt
// from a summary would change the agent's own instructions.
//
// Every stage leaves a conversation a provider will accept. Tool results are
// emptied rather than removed, because a tool message carries the id that
// answers a call, and a call left unanswered fails validation before the model
// sees it.
func CompactMessages(messages []provider.Message, keepTurns, targetTokens int, summarize Summarizer) (Compaction, error) {
	original := estimateTokens(messages)
	result := Compaction{Messages: messages, Stage: StageNone}
	if original <= targetTokens {
		return result, nil
	}

	head, tail := splitAtRecentTurns(messages, keepTurns)
	if len(head) == 0 {
		// Everything is recent. There is nothing this transform may touch, and
		// pretending otherwise would break the one guarantee it makes.
		return result, nil
	}

	working := append([]provider.Message(nil), head...)
	replaced := 0

	// 1. Tool output: most of the bytes of a coding session, least of its
	// meaning, and already capped at the tool layer.
	for i := range working {
		if working[i].Role != "tool" || working[i].Content == "" {
			continue
		}
		if strings.HasPrefix(working[i].Content, "[tool output dropped") {
			continue
		}
		working[i].Content = fmt.Sprintf("[tool output dropped: %d chars]", len(working[i].Content))
		replaced++
	}
	if fits(working, tail, targetTokens) {
		return finish(working, tail, StageToolResults, replaced, original), nil
	}

	// 2. The calls themselves, collapsed with their results into one line that
	// still records what ran.
	collapsed, collapsedCount := collapseToolTraffic(working)
	if collapsedCount > 0 {
		replaced += collapsedCount
		working = collapsed
	}
	if fits(working, tail, targetTokens) {
		return finish(working, tail, StageToolCalls, replaced, original), nil
	}

	// 3. Everything older becomes one summary.
	if summarize == nil {
		// Without a summarizer this is as small as it gets. Report honestly
		// rather than claiming a stage that did not run.
		stage := StageToolCalls
		if collapsedCount == 0 {
			stage = StageToolResults
		}
		return finish(working, tail, stage, replaced, original), nil
	}
	summary, err := summarize(working)
	if err != nil {
		return Compaction{}, fmt.Errorf("summarising the older conversation: %w", err)
	}
	kept := []provider.Message{}
	if len(working) > 0 && working[0].Role == "system" {
		kept = append(kept, working[0])
	}
	kept = append(kept, provider.Message{
		Role:    "assistant",
		Content: "[earlier conversation, summarised]\n" + summary,
	})
	replaced = len(working) - len(kept)
	return finish(kept, tail, StageSummary, replaced, original), nil
}

func finish(head, tail []provider.Message, stage string, replaced, original int) Compaction {
	messages := append(append([]provider.Message(nil), head...), tail...)
	return Compaction{
		Messages:    messages,
		Stage:       stage,
		Replaced:    replaced,
		FreedTokens: original - estimateTokens(messages),
	}
}

func fits(head, tail []provider.Message, target int) bool {
	return estimateTokens(head)+estimateTokens(tail) <= target
}

// splitAtRecentTurns returns everything that may be compacted, and the recent
// turns that may not. A turn starts at a user message.
func splitAtRecentTurns(messages []provider.Message, keepTurns int) (head, tail []provider.Message) {
	if keepTurns <= 0 {
		return messages, nil
	}
	seen, boundary := 0, -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		seen++
		if seen == keepTurns {
			boundary = i
			break
		}
	}
	if boundary <= 0 {
		return nil, messages
	}
	return messages[:boundary], messages[boundary:]
}

// collapseToolTraffic replaces each assistant tool call and its results with a
// single line naming what ran, which keeps the history valid while removing the
// arguments and the answers.
func collapseToolTraffic(messages []provider.Message) ([]provider.Message, int) {
	out := make([]provider.Message, 0, len(messages))
	collapsed := 0
	for i := 0; i < len(messages); i++ {
		message := messages[i]
		if len(message.ToolCalls) == 0 {
			out = append(out, message)
			continue
		}
		names := make([]string, 0, len(message.ToolCalls))
		for _, call := range message.ToolCalls {
			names = append(names, call.Function.Name)
		}
		answered := map[string]bool{}
		for _, call := range message.ToolCalls {
			answered[call.ID] = true
		}
		skipped := 0
		for j := i + 1; j < len(messages); j++ {
			if messages[j].Role != "tool" || !answered[messages[j].ToolCallID] {
				break
			}
			skipped++
		}
		out = append(out, provider.Message{
			Role:    "assistant",
			Content: fmt.Sprintf("[ran: %s]", strings.Join(names, ", ")),
		})
		collapsed += 1 + skipped
		i += skipped
	}
	return out, collapsed
}

func estimateTokens(messages []provider.Message) int {
	characters := 0
	for _, message := range messages {
		characters += len(message.Content)
		for _, call := range message.ToolCalls {
			characters += len(call.Function.Name) + len(call.Function.Arguments)
		}
	}
	return characters / charsPerToken
}
