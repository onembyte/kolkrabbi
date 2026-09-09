package local

import (
	"context"
	"strings"
	"sync"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// Backend is what a local endpoint answers turns with: the engine's own
// chat contract, named here so this package need not import the engine.
type Backend interface {
	StreamChat(context.Context, string, []provider.Message, []provider.Tool, func(string)) (provider.Message, provider.Meta, error)
}

// EndpointBackend answers for one local endpoint (plan 37). An Ollama keeps
// the host backend it has always had, which learns each model's window from
// the server; anything OpenAI-compatible speaks to its base URL with no key,
// because a local endpoint is never sent one.
func EndpointBackend(kind, base string) Backend {
	if kind == KindOllama {
		// The host backend takes an address and adds the /v1 itself.
		return NewHostBackend(strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(base, "http://"), "https://"), "/v1"))
	}
	return newCompatibleBackend(base)
}

// compatibleBackend is a chat backend over any OpenAI-compatible base URL.
type compatibleBackend struct {
	base string

	once   sync.Once
	client *provider.Client
}

func newCompatibleBackend(base string) *compatibleBackend {
	return &compatibleBackend{base: base}
}

func (b *compatibleBackend) StreamChat(ctx context.Context, model string, messages []provider.Message, tools []provider.Tool, onToken func(string)) (provider.Message, provider.Meta, error) {
	b.once.Do(func() { b.client = provider.NewCompatibleClient(b.base) })
	message, meta, err := b.client.StreamChat(ctx, model, messages, tools, onToken)
	if meta.Billing == "" {
		meta.Billing = provider.BillingLocal
	}
	return message, meta, err
}

// ContextWindow is unknown here: an OpenAI-compatible list says what models
// exist, not how much context each one takes. Zero means "kolk decides",
// which is better than a number nobody measured.
func (b *compatibleBackend) ContextWindow(string) int { return 0 }
