package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

func (a *app) runConfig(ctx context.Context, args []string) error {
	// Keep the old spelling as a hard, side-effect-free redirect. Delegating
	// would leave two supported key commands forever and would bypass the one
	// provider-agnostic command's CI and shape guidance.
	if len(args) > 0 && args[0] == "set-key" {
		return usagef("`kolk config set-key` was replaced; use `kolk key <API_KEY>`")
	}

	d, err := a.resolve()
	if err != nil {
		return err
	}
	if configWriteCommand(args) {
		if err := a.migrateLegacyCredential(ctx, d); err != nil {
			return err
		}
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
		fmt.Fprintf(a.stdout, "model:    %s\nbase_url: %s\n",
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

func configWriteCommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "set-model", "set-base-url":
		return len(args) >= 2
	case "set-tier":
		return len(args) >= 3 && validEffort(args[1])
	default:
		return false
	}
}

func validEffort(s string) bool {
	for _, e := range engine.Efforts {
		if e == s {
			return true
		}
	}
	return false
}
