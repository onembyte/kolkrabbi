package cli

import (
	"context"
	"fmt"

	"github.com/onembyte/kolkrabbi/internal/keystore"
	"github.com/onembyte/kolkrabbi/internal/paths"
)

// migrateLegacyCredential is called only from commands that are already
// writing state. Read-only env resolution must never need a home directory or
// trigger a migration as a side effect.
func (a *app) migrateLegacyCredential(ctx context.Context, dirs paths.Dirs) error {
	moved, err := keystore.NewFileStore(dirs.CredentialsFile()).MigrateLegacyConfig(ctx, dirs.ConfigFile())
	if err != nil {
		return err
	}
	if moved {
		fmt.Fprintf(a.stderr, "moved your saved API key from %s to %s\n", dirs.ConfigFile(), dirs.CredentialsFile())
	}
	return nil
}
