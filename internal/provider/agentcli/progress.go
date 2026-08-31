package agentcli

import "github.com/onembyte/kolkrabbi/internal/provider"

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
		if event.ToolName != "" {
			if event.ToolCallID != "" && pending != nil {
				pending[event.ToolCallID] = event.ToolName
			}
			observe(provider.ProgressEvent{
				Kind: provider.ProgressToolStarted, ID: event.ToolCallID,
				Name: event.ToolName, Detail: oneLine(event.ToolInput, 100),
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
			})
		}
	case EventError:
		if event.Error != "" {
			observe(provider.ProgressEvent{Kind: provider.ProgressError, Detail: oneLine(event.Error, 100), Error: true})
		}
	case EventLimit:
		observe(provider.ProgressEvent{Kind: provider.ProgressLimit, Detail: oneLine(limitTrail(event), 100), Error: event.LimitRejected})
	}
}
