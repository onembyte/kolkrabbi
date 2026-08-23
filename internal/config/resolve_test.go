package config

import (
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

func TestResolveBaseURL(t *testing.T) {
	cases := []struct {
		name string
		flag string
		env  string
		cfg  *Config
		want string
	}{
		{name: "nothing set falls back to OpenRouter", cfg: &Config{}, want: provider.DefaultBaseURL},
		{name: "nil config is not a crash", cfg: nil, want: provider.DefaultBaseURL},
		{name: "config is used", cfg: &Config{BaseURL: "http://cfg"}, want: "http://cfg"},
		{name: "env beats config", env: "http://env", cfg: &Config{BaseURL: "http://cfg"}, want: "http://env"},
		{name: "flag beats env", flag: "http://flag", env: "http://env", cfg: &Config{BaseURL: "http://cfg"}, want: "http://flag"},
		{name: "trailing slashes are trimmed", flag: "http://flag//", cfg: &Config{}, want: "http://flag"},
		{name: "empty env is ignored", env: "", cfg: &Config{BaseURL: "http://cfg"}, want: "http://cfg"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("OPENROUTER_BASE_URL", c.env)
			if got := ResolveBaseURL(c.flag, c.cfg); got != c.want {
				t.Errorf("ResolveBaseURL(%q, %+v) = %q, want %q", c.flag, c.cfg, got, c.want)
			}
		})
	}
}

func TestResolveAPIKeyPrefersEnv(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "env-key")
	if got := ResolveAPIKey(&Config{APIKey: "cfg-key"}); got != "env-key" {
		t.Errorf("ResolveAPIKey = %q, want env-key", got)
	}
	t.Setenv("OPENROUTER_API_KEY", "")
	if got := ResolveAPIKey(&Config{APIKey: "cfg-key"}); got != "cfg-key" {
		t.Errorf("ResolveAPIKey = %q, want cfg-key", got)
	}
}
