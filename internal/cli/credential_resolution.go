package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/onembyte/kolkrabbi/internal/keystore"
	"github.com/onembyte/kolkrabbi/internal/secret"
)

// resolveOpenRouterCredential implements the two sources supported by the
// first owner trial. The environment override is deliberately checked before
// constructing a store, so an env-only run does not inspect disk and cannot
// be broken by stale or corrupt manifest state.
func resolveOpenRouterCredential(ctx context.Context, manifestPath string) (secret.Secret, error) {
	if value := secret.New(os.Getenv("OPENROUTER_API_KEY")); !value.IsZero() {
		return value, nil
	}

	ref := keystore.Ref{Provider: "openrouter", Profile: "default"}
	value, err := keystore.NewFileStore(manifestPath).Get(ctx, ref)
	switch {
	case err == nil:
		return value, nil
	case errors.Is(err, keystore.ErrNotFound):
		return secret.Secret{}, nil
	default:
		return secret.Secret{}, fmt.Errorf("reading saved credential %s: %w", manifestPath, secret.ScrubError(err))
	}
}
