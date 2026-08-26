package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/onembyte/kolkrabbi/internal/atomicfile"
	"github.com/onembyte/kolkrabbi/internal/lock"
)

const connectorManifestVersion = 1

// Connector records how kolk may invoke a provider-owned CLI. It deliberately
// contains no token, cookie, credential path, or refresh state.
type Connector struct {
	Provider   string    `json:"provider"`
	Plan       string    `json:"plan"`
	Name       string    `json:"name"`
	Sandbox    bool      `json:"sandbox"`
	LoginOwner string    `json:"login_owner"`
	Enabled    bool      `json:"enabled"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type ConnectorManifest struct {
	Version    int         `json:"version"`
	Connectors []Connector `json:"connectors"`
}

func LoadConnectors(path string) (ConnectorManifest, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return ConnectorManifest{Version: connectorManifestVersion}, nil
	}
	if err != nil {
		return ConnectorManifest{}, fmt.Errorf("reading connector manifest %s: %w", path, err)
	}
	var manifest ConnectorManifest
	if len(b) == 0 || json.Unmarshal(b, &manifest) != nil {
		return ConnectorManifest{}, fmt.Errorf("%s is not valid connector JSON", path)
	}
	if manifest.Version != connectorManifestVersion {
		return ConnectorManifest{}, fmt.Errorf("%s has unsupported connector manifest version %d", path, manifest.Version)
	}
	if manifest.Connectors == nil {
		manifest.Connectors = []Connector{}
	}
	return manifest, nil
}

// SaveConnector atomically upserts one non-secret connector record and locks
// the read-modify-write sequence for concurrent kolk processes.
func SaveConnector(ctx context.Context, path string, connector Connector) error {
	if err := validateConnector(connector); err != nil {
		return err
	}
	// The manifest promises when each connector was last written. Callers that
	// do not care get the current instant; callers replaying a known write keep
	// theirs, so the field is never silently zero.
	if connector.UpdatedAt.IsZero() {
		connector.UpdatedAt = time.Now().UTC()
	} else {
		connector.UpdatedAt = connector.UpdatedAt.UTC()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating connector directory: %w", err)
	}
	held, err := lock.Acquire(ctx, path+".lock")
	if err != nil {
		return err
	}
	defer func() { _ = held.Close() }()

	manifest, err := LoadConnectors(path)
	if err != nil {
		return err
	}
	replaced := false
	for i := range manifest.Connectors {
		if manifest.Connectors[i].Provider == connector.Provider &&
			manifest.Connectors[i].Name == connector.Name {
			manifest.Connectors[i] = connector
			replaced = true
			break
		}
	}
	if !replaced {
		manifest.Connectors = append(manifest.Connectors, connector)
	}
	manifest.Version = connectorManifestVersion
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding connector manifest: %w", err)
	}
	return atomicfile.Write(path, append(data, '\n'), 0o600)
}

func validateConnector(c Connector) error {
	if c.Provider == "" || c.Plan == "" || c.Name == "" || c.LoginOwner == "" {
		return errors.New("connector metadata requires provider, plan, name, and login owner")
	}
	if c.LoginOwner != "provider-cli" {
		return fmt.Errorf("unsupported connector login owner %q", c.LoginOwner)
	}
	return nil
}
