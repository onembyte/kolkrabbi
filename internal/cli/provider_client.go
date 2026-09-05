package cli

import (
	"context"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// providerClientForEndpoint resolves credential requirements from the endpoint
// before touching OpenRouter's credential store. Compatible endpoints are
// deliberately keyless; only canonical OpenRouter loads and receives its key.
func providerClientForEndpoint(ctx context.Context, endpoint, credentialPath string) (*provider.Client, error) {
	// Before the keyed/keyless decision: a URL carrying credentials is refused
	// whatever host it names (V34.1d.3).
	if err := provider.RefuseCredentialedEndpoint(endpoint); err != nil {
		return nil, err
	}
	if !provider.IsOpenRouterEndpoint(endpoint) {
		return provider.NewCompatibleClient(endpoint), nil
	}

	apiKey, err := resolveOpenRouterCredential(ctx, credentialPath)
	if err != nil {
		return nil, err
	}
	if apiKey.IsZero() {
		return nil, guidedAction("kolk needs an API key before it can use models.\n" +
			"Add one:  /key   (it asks for the key, hidden)\n" +
			"Then run: kolk")
	}
	return provider.NewOpenRouterClient(endpoint, apiKey.Reveal())
}
