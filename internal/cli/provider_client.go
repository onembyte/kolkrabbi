package cli

import (
	"context"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// providerClientForEndpoint resolves credential requirements from the endpoint
// before touching OpenRouter's credential store. Compatible endpoints are
// deliberately keyless; only canonical OpenRouter loads and receives its key.
func providerClientForEndpoint(ctx context.Context, endpoint, credentialPath string) (*provider.Client, error) {
	if !provider.IsOpenRouterEndpoint(endpoint) {
		return provider.NewCompatibleClient(endpoint), nil
	}

	apiKey, err := resolveOpenRouterCredential(ctx, credentialPath)
	if err != nil {
		return nil, err
	}
	if apiKey.IsZero() {
		return nil, guidedAction("kolk needs an API key before it can use models.\n" +
			"Add one:  /key <API_KEY>\n" +
			"Then run: kolk")
	}
	return provider.NewOpenRouterClient(endpoint, apiKey.Reveal())
}
