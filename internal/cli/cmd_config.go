package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

func (a *app) runConfig(_ context.Context, args []string) error {
	d, err := a.resolve()
	if err != nil {
		return err
	}
	cfg, err := config.Load(d.ConfigFile())
	if err != nil {
		return err
	}
	if len(args) == 0 {
		fmt.Fprintln(a.stdout, usageLine("config"))
		return nil
	}

	switch args[0] {
	case "set-key":
		if len(args) < 2 {
			return usagef("usage: kolk config set-key <key>")
		}
		cfg.APIKey = args[1]
		if err := config.Save(d.ConfigFile(), cfg); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "API key saved to %s\n", d.ConfigFile())

	case "set-model":
		if len(args) < 2 {
			return usagef("usage: kolk config set-model <model>")
		}
		cfg.Model = strings.Join(args[1:], " ")
		if err := config.Save(d.ConfigFile(), cfg); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "default model set to %s\n", cfg.Model)

	case "set-base-url":
		if len(args) < 2 {
			return usagef("usage: kolk config set-base-url <url>")
		}
		cfg.BaseURL = strings.TrimRight(args[1], "/")
		if err := config.Save(d.ConfigFile(), cfg); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "base URL set to %s\n", cfg.BaseURL)

	case "set-tier":
		if len(args) < 3 {
			return usagef("usage: kolk config set-tier <quick|standard|deep|ultra> <model>")
		}
		if !validEffort(args[1]) {
			return usagef("unknown effort %q (quick|standard|deep|ultra)", args[1])
		}
		if cfg.Tiers == nil {
			cfg.Tiers = map[string]string{}
		}
		cfg.Tiers[args[1]] = args[2]
		if err := config.Save(d.ConfigFile(), cfg); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "tier %s → %s\n", args[1], args[2])

	case "show":
		key := "(not set)"
		if cfg.APIKey != "" {
			key = maskKey(cfg.APIKey)
		}
		fmt.Fprintf(a.stdout, "api_key:  %s\nmodel:    %s\nbase_url: %s\n",
			key,
			orDefault(cfg.Model, defaultModel+" (default)"),
			orDefault(cfg.BaseURL, provider.DefaultBaseURL+" (default)"))
		if len(cfg.Tiers) == 0 {
			fmt.Fprintln(a.stdout, "tiers:    (none — all efforts use the session model; set with `kolk config set-tier`)")
			break
		}
		fmt.Fprintln(a.stdout, "tiers:")
		for _, e := range engine.Efforts {
			if m, ok := cfg.Tiers[e]; ok {
				fmt.Fprintf(a.stdout, "  %-9s %s\n", e, m)
			}
		}

	default:
		return usagef("%s", usageLine("config"))
	}
	return nil
}

func validEffort(s string) bool {
	for _, e := range engine.Efforts {
		if e == s {
			return true
		}
	}
	return false
}

// maskKey shows just enough of a key to recognise it. It moves to
// internal/secret at migration step 5, where it also starts running over
// sessions, stats and logs rather than only this one screen.
func maskKey(k string) string {
	if len(k) <= 8 {
		return "****"
	}
	return k[:6] + "…" + k[len(k)-4:]
}
