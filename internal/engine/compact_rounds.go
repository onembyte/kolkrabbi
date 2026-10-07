package engine

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// completeToolRound returns the first index after a contiguous, fully answered
// transaction. A partly executed batch must survive byte for byte for resumption.
func completeToolRound(messages []provider.Message, start int) int {
	if messages[start].Role != "assistant" || len(messages[start].ToolCalls) == 0 {
		return start
	}
	pending := make(map[string]bool)
	for _, call := range messages[start].ToolCalls {
		if call.ID == "" || pending[call.ID] {
			return start
		}
		pending[call.ID] = true
	}
	end := start + 1
	for end < len(messages) && messages[end].Role == "tool" {
		if !pending[messages[end].ToolCallID] {
			return start
		}
		delete(pending, messages[end].ToolCallID)
		end++
	}
	if len(pending) != 0 {
		return start
	}
	return end
}

// shortenEvidence keeps both ends, where paths, headers and final diagnostics
// commonly live. It is explicitly an excerpt; the complete text is archived.
func shortenEvidence(value string, limit int) string {
	if len(value) <= limit || limit < 80 {
		return value
	}
	marker := "\n[excerpt; full text retained in session history]\n"
	head := (limit - len(marker)) / 2
	tail := len(value) - (limit - len(marker) - head)
	for head > 0 && !utf8.RuneStart(value[head]) {
		head--
	}
	for tail < len(value) && !utf8.RuneStart(value[tail]) {
		tail++
	}
	return value[:head] + marker + value[tail:]
}

func toolRoundDigest(messages []provider.Message, start, end int) provider.Message {
	var b strings.Builder
	b.WriteString(messages[start].Content)
	b.WriteString("\n[completed tool round; full details retained in session history]")
	for _, call := range messages[start].ToolCalls {
		fmt.Fprintf(&b, "\n%s %s", call.Function.Name, shortenEvidence(call.Function.Arguments, 384))
		for _, answer := range messages[start+1 : end] {
			if answer.ToolCallID == call.ID {
				fmt.Fprintf(&b, "\nResult: %s", shortenEvidence(answer.Content, 512))
			}
		}
	}
	return provider.Message{Role: "assistant", Content: b.String()}
}

// compactToolRounds can shrink a single long turn without summarizing away its
// goal, instructions or assistant decisions. The newest transaction is retained
// intact when older traffic suffices; its output is excerpted only as a last step.
func compactToolRounds(messages []provider.Message, target int) Compaction {
	original := estimateTokens(messages)
	working := append([]provider.Message(nil), messages...)
	latest := -1
	for i := range working {
		if completeToolRound(working, i) > i {
			latest = i
		}
	}
	replaced := 0
	for pass := 0; pass < 3 && estimateTokens(working) > target; pass++ {
		for i := 0; i < len(working) && estimateTokens(working) > target; i++ {
			end := completeToolRound(working, i)
			if end == i || (pass < 2 && i == latest) || (pass == 2 && i != latest) {
				continue
			}
			if pass == 1 {
				digest := toolRoundDigest(working, i, end)
				if estimateTokens([]provider.Message{digest}) < estimateTokens(working[i:end]) {
					working = append(append(working[:i:i], digest), working[end:]...)
					latest -= end - i - 1
					replaced += end - i
					continue
				}
			} else {
				for j := i + 1; j < end; j++ {
					shorter := shortenEvidence(working[j].Content, 1024)
					if len(shorter) < len(working[j].Content) {
						working[j].Content = shorter
						replaced++
					}
				}
			}
			i = end - 1
		}
	}
	return finish(working, nil, StageToolResults, replaced, original)
}

func overflowTarget(window int, messages []provider.Message) int {
	target := estimateTokens(messages) / 2
	if window > 0 {
		target = min(target, window/2)
	}
	return target
}
