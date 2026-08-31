package provider

import "context"

// ProgressKind is one provider-owned boundary observed during a streamed turn.
// It is deliberately separate from Kolkrabbi's protocol events: a backend may
// expose richer vendor facts, while the engine decides which become durable
// work or tool records.
type ProgressKind string

const (
	ProgressMessage      ProgressKind = "message"
	ProgressToolStarted  ProgressKind = "tool.started"
	ProgressToolFinished ProgressKind = "tool.finished"
	ProgressError        ProgressKind = "error"
	ProgressLimit        ProgressKind = "limit"
)

// ProgressEvent is a scrubbed, provider-executed boundary. Detail is a short
// display-safe preview; complete output remains in the backend's own result
// handling and must not be treated as an instruction to execute a tool again.
type ProgressEvent struct {
	Kind   ProgressKind
	ID     string
	Name   string
	Detail string
	Error  bool
}

// ObservedChatBackend is an optional extension to the regular streaming seam.
// Consumers must use a type assertion: HTTP/gateway and test backends retain
// their original StreamChat implementation unchanged.
type ObservedChatBackend interface {
	StreamChatObserved(context.Context, string, []Message, []Tool, func(string), func(ProgressEvent)) (Message, Meta, error)
}
