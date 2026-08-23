package config

import (
	"os"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// ResolveBaseURL picks the endpoint for this run, most specific first:
// the --base-url flag, then OPENROUTER_BASE_URL, then the saved config, then
// OpenRouter itself. Every layer is optional, so pointing kolk at Ollama is one
// flag and pointing it back is deleting one.
func ResolveBaseURL(flagVal string, cfg *Config) string {
	if flagVal != "" {
		return strings.TrimRight(flagVal, "/")
	}
	if v := os.Getenv("OPENROUTER_BASE_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	if cfg != nil && cfg.BaseURL != "" {
		return strings.TrimRight(cfg.BaseURL, "/")
	}
	return provider.DefaultBaseURL
}
