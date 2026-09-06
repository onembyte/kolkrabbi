package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/onembyte/kolkrabbi/internal/keystore"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/secret"
	"github.com/onembyte/kolkrabbi/internal/shell"
)

// resolveCredential walks plan 05's chain for one provider: KOLK_API_KEY,
// the provider's own variable, then the store. Nothing found is an empty
// secret, not an error; a store that is locked, unavailable or slow is an
// error with its own remedy, never mistaken for "no key".
func resolveCredential(ctx context.Context, providerName, manifestPath string) (secret.Secret, error) {
	ref := keystore.Ref{Provider: providerName, Profile: "default"}
	res, err := keystore.Resolve(ctx, ref, os.Getenv, routedStore(manifestPath, nil))
	switch {
	case err == nil:
		return res.Value, nil
	case errors.Is(err, keystore.ErrNotFound):
		return secret.Secret{}, nil
	default:
		return secret.Secret{}, fmt.Errorf("reading the saved %s credential: %w", providerName, secret.ScrubError(err))
	}
}

func resolveOpenRouterCredential(ctx context.Context, manifestPath string) (secret.Secret, error) {
	return resolveCredential(ctx, "openrouter", manifestPath)
}

// resolveVendorCredential is the same chain for an owner-chosen vendor
// origin; the vendor's own variable is the curated one (keystore.ProviderEnv),
// which the disposition names too.
func resolveVendorCredential(ctx context.Context, vendor, manifestPath string) (secret.Secret, error) {
	if env := provider.VendorKeyEnv(vendor); env != "" && keystore.ProviderEnv(vendor) != env {
		// The disposition and the curated list must agree; a drift is a bug
		// worth failing loudly on rather than reading the wrong variable.
		return secret.Secret{}, fmt.Errorf("%s: the disposition names %s but the curated list names %q", vendor, env, keystore.ProviderEnv(vendor))
	}
	return resolveCredential(ctx, vendor, manifestPath)
}

// keyStoreAdvice is the remedy for a store that could not answer — locked,
// unavailable, or slow — in the words plan 05 §1.2 requires: named, and
// never "no key". Empty for any other error.
func keyStoreAdvice(err error) string {
	switch {
	case errors.Is(err, keystore.ErrLocked):
		return "the credential store is locked: unlock it (on macOS, `security unlock-keychain`) and run this again; kolk will not guess another source while it is locked"
	case errors.Is(err, keystore.ErrTimeout):
		return "the credential store did not answer in time; try again, and if it keeps timing out move the key with `kolk key --backend file`"
	case errors.Is(err, keystore.ErrUnavailable):
		return "the credential store is unavailable on this machine; move the key with `kolk key --backend file`, or set the provider's own variable for this session"
	}
	return ""
}

// routedStore is the store the chain reads: the manifest routing each
// credential to the file or, opt-in, the OS keychain. A nil spawner means
// the real one from the shell package.
func routedStore(manifestPath string, spawn keystore.Spawner) *keystore.Routed {
	manifest := keystore.NewFileStore(manifestPath)
	if spawn == nil {
		spawn = shell.SecretSpawner{}
	}
	return keystore.NewRouted(manifest, keystore.NewKeychainStore(manifest, spawn))
}
