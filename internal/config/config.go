// Package config handles kolk's persistent settings: the default model, the
// endpoint, and the effort tiers.
//
// Every setting here is an override for a default that already works. Nothing
// in this package may become required reading for a new user — a config file
// someone has to open before kolk does anything useful is the product failing,
// not the file being incomplete.
//
// The package takes the file path rather than computing it. Locating
// directories belongs to internal/paths and nowhere else, which is what keeps
// the answer to "where does my config live?" a single one.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/onembyte/kolkrabbi/internal/atomicfile"
)

type Config struct {
	Model   string            `json:"model"`
	BaseURL string            `json:"base_url,omitempty"`
	Tiers   map[string]string `json:"tiers,omitempty"` // effort level -> model id
}

// Load reads a config file. A missing file is not an error: it returns a
// zero-value Config, because "no config" is the supported, expected state.
func Load(file string) (*Config, error) {
	b, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		// Name the file. A parse error with no path is a scavenger hunt.
		return nil, fmt.Errorf("%s is not valid JSON: %w", file, err)
	}
	return &cfg, nil
}

// Save writes a config file, creating its directory if needed.
//
// 0600 on the file and 0700 on the directory keep user preferences private.
// The prototype stored an API key here, but the Config schema no longer has a
// field capable of writing one; keystore.MigrateLegacyConfig owns evacuation.
func Save(file string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	// Atomic: a config file truncated by a crash mid-write silently forgets the
	// user's model and endpoint choices.
	return atomicfile.Write(file, append(b, '\n'), 0o600)
}
