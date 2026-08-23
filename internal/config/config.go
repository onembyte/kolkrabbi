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
)

type Config struct {
	APIKey  string            `json:"api_key"`
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
// 0600 on the file and 0700 on the directory: the prototype stored the API key
// here and existing installs still do, so this file has to be treated as
// holding a secret whether or not it currently does.
func Save(file string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(file, append(b, '\n'), 0o600)
}

// ResolveAPIKey prefers the OPENROUTER_API_KEY env var over the saved config,
// so a shell session or CI job can override it without touching disk.
func ResolveAPIKey(cfg *Config) string {
	if v := os.Getenv("OPENROUTER_API_KEY"); v != "" {
		return v
	}
	if cfg == nil {
		return ""
	}
	return cfg.APIKey
}
