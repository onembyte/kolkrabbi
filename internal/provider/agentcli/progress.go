package agentcli

import (
	"unicode/utf8"

	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/secret"
)

var (
	_ provider.ObservedChatBackend = (*ClaudeBackend)(nil)
	_ provider.ObservedChatBackend = (*CodexBackend)(nil)
)

// observeProviderEvent keeps the typed provider boundary alongside the legacy
// human trail. pending belongs to one stream, so a completion that names only
// an id still reaches the engine with the tool name that started it.
func observeProviderEvent(observe func(provider.ProgressEvent), event Event, pending map[string]string) {
	if observe == nil {
		return
	}
	switch event.Kind {
	case EventMessageDelta:
		if event.Text != "" {
			observe(provider.ProgressEvent{Kind: provider.ProgressMessage, Detail: event.Text})
		}
	case EventTool:
		if event.ToolName == "codex-warning" {
			observe(provider.ProgressEvent{Kind: provider.ProgressWarning, Name: "codex", Input: progressBody(event.ToolInput)})
			return
		}
		if event.ToolName != "" {
			if event.ToolCallID != "" && pending != nil {
				pending[event.ToolCallID] = event.ToolName
			}
			observe(provider.ProgressEvent{
				Kind: provider.ProgressToolStarted, ID: event.ToolCallID,
				Name: event.ToolName, Detail: oneLine(event.ToolInput, 100),
				Input: progressBody(event.ToolInput),
			})
			return
		}
		if event.ToolCallID != "" {
			name := ""
			if pending != nil {
				name = pending[event.ToolCallID]
			}
			observe(provider.ProgressEvent{
				Kind: provider.ProgressToolFinished, ID: event.ToolCallID,
				Name: name, Detail: oneLine(event.ToolOutput, 100), Error: event.ToolIsError,
				Output: progressBody(event.ToolOutput),
			})
		}
	case EventError:
		if event.Error != "" {
			observe(provider.ProgressEvent{Kind: provider.ProgressError, Detail: oneLine(event.Error, 100), Error: true})
		}
	case EventLimit:
		// A plain reading of a window is not news; a warning and a rejection
		// are. The reading still rides on Meta for the status meters.
		if !event.LimitWarning && !event.LimitRejected {
			return
		}
		observe(provider.ProgressEvent{Kind: provider.ProgressLimit, Detail: oneLine(limitTrail(event), 100), Error: event.LimitRejected})
	}
}

func progressBody(text string) string {
	text = secret.Scrub(text)
	const limit = 64 << 10
	if len(text) <= limit {
		return text
	}
	end := limit
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	return text[:end] + "\n… output excerpt truncated"
}
