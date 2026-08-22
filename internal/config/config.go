// Package config handles kolk's persistent settings: the OpenRouter API key
// and default model, stored at ~/.config/kolk/config.json.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	APIKey  string            `json:"api_key"`
	Model   string            `json:"model"`
	BaseURL string            `json:"base_url,omitempty"`
	Tiers   map[string]string `json:"tiers,omitempty"` // effort level -> model id
}

func dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "kolk"), nil
}

func path() (string, error) {
	d, err := dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "config.json"), nil
}

// Load reads the config file. A missing file is not an error; it returns a
// zero-value Config so callers can fall back to environment variables.
func Load() (*Config, error) {
	p, err := path()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func Save(cfg *Config) error {
	d, err := dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(d, 0o700); err != nil {
		return err
	}
	p, err := path()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

// ResolveAPIKey prefers the OPENROUTER_API_KEY env var over the saved config,
// so a shell session or CI job can override it without touching disk.
func ResolveAPIKey(cfg *Config) string {
	if v := os.Getenv("OPENROUTER_API_KEY"); v != "" {
		return v
	}
	return cfg.APIKey
}
