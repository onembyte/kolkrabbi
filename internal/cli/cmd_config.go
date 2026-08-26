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
	case "get":
		if len(args) < 2 {
			return usagef("usage: kolk config get <key>")
		}
		key := args[1]
		switch {
		case key == "model" || key == "model.default":
			if cfg.Model != "" {
				fmt.Fprintln(a.stdout, cfg.Model)
			} else {
				fmt.Fprintf(a.stdout, "(unset — inherits %s)\n", defaultModel)
			}
		case key == "base_url":
			if cfg.BaseURL != "" {
				fmt.Fprintln(a.stdout, cfg.BaseURL)
			} else {
				fmt.Fprintf(a.stdout, "(unset — inherits %s)\n", provider.DefaultBaseURL)
			}
		case strings.HasPrefix(key, "effort.") || strings.HasPrefix(key, "tier."):
			canonical, err := parseEffortKey(key)
			if err != nil {
				return err
			}
			if m, ok := cfg.Tiers[canonical]; ok && m != "" {
				fmt.Fprintln(a.stdout, m)
			} else {
				fmt.Fprintf(a.stdout, "(unset — inherits model %s)\n", orDefault(cfg.Model, defaultModel))
			}
		default:
			return usagef("unknown config key %q", key)
		}

	case "set":
		if len(args) < 3 {
			return usagef("usage: kolk config set <key> <value>")
		}
		key := args[1]
		val := strings.Join(args[2:], " ")
		switch {
		case key == "model" || key == "model.default":
			cfg.Model = val
			if err := config.Save(d.ConfigFile(), cfg); err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "model → %s\n", val)
		case key == "base_url":
			cfg.BaseURL = strings.TrimRight(val, "/")
			if err := config.Save(d.ConfigFile(), cfg); err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "base_url → %s\n", cfg.BaseURL)
		case strings.HasPrefix(key, "effort.") || strings.HasPrefix(key, "tier."):
			canonical, err := parseEffortKey(key)
			if err != nil {
				return err
			}
			if cfg.Tiers == nil {
				cfg.Tiers = map[string]string{}
			}
			cfg.Tiers[canonical] = val
			if err := config.Save(d.ConfigFile(), cfg); err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "effort.%s.model → %s\n", canonical, val)
		default:
			return usagef("unknown config key %q", key)
		}

	case "unset":
		if len(args) < 2 {
			return usagef("usage: kolk config unset <key>")
		}
		key := args[1]
		switch {
		case key == "model" || key == "model.default":
			cfg.Model = ""
			if err := config.Save(d.ConfigFile(), cfg); err != nil {
				return err
			}
			fmt.Fprintln(a.stdout, "removed model")
		case key == "base_url":
			cfg.BaseURL = ""
			if err := config.Save(d.ConfigFile(), cfg); err != nil {
				return err
			}
			fmt.Fprintln(a.stdout, "removed base_url")
		case strings.HasPrefix(key, "effort.") || strings.HasPrefix(key, "tier."):
			canonical, err := parseEffortKey(key)
			if err != nil {
				return err
			}
			if cfg.Tiers != nil {
				delete(cfg.Tiers, canonical)
				for _, leg := range []string{"quick", "standard", "deep", "ultra"} {
					if c, _ := engine.NormalizeEffort(leg); c == canonical {
						delete(cfg.Tiers, leg)
					}
				}
			}
			if err := config.Save(d.ConfigFile(), cfg); err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "removed effort.%s.model\n", canonical)
		default:
			return usagef("unknown config key %q", key)
		}

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
			return usagef("usage: kolk config set-tier <low|medium|high|max> <model>")
		}
		canonical, ok := engine.NormalizeEffort(args[1])
		if !ok {
			return usagef("unknown effort %q (low|medium|high|max)", args[1])
		}
		if cfg.Tiers == nil {
			cfg.Tiers = map[string]string{}
		}
		cfg.Tiers[canonical] = args[2]
		if err := config.Save(d.ConfigFile(), cfg); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "tier %s → %s\n", canonical, args[2])

	case "show":
		fmt.Fprintf(a.stdout, "model:    %s\nbase_url: %s\n",
			orDefault(cfg.Model, defaultModel+" (default)"),
			orDefault(cfg.BaseURL, provider.DefaultBaseURL+" (default)"))
		if len(cfg.Tiers) == 0 {
			fmt.Fprintln(a.stdout, "tiers:    (none — all efforts use the session model; set with `kolk config set-tier`)")
			break
		}
		fmt.Fprintln(a.stdout, "tiers:")
		for _, e := range engine.CanonicalEfforts {
			if m, ok := cfg.Tiers[e]; ok {
				fmt.Fprintf(a.stdout, "  %-9s %s\n", e, m)
			}
		}
		for _, e := range []string{"quick", "standard", "deep", "ultra"} {
			if m, ok := cfg.Tiers[e]; ok {
				c, _ := engine.NormalizeEffort(e)
				if _, canonicalSet := cfg.Tiers[c]; !canonicalSet {
					fmt.Fprintf(a.stdout, "  %-9s %s\n", e, m)
				}
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
	case "set":
		return len(args) >= 3
	case "unset":
		return len(args) >= 2
	default:
		return false
	}
}

func parseEffortKey(key string) (string, error) {
	parts := strings.Split(key, ".")
	var level string
	if len(parts) == 3 && parts[0] == "effort" && parts[2] == "model" {
		level = parts[1]
	} else if len(parts) == 2 && (parts[0] == "tier" || parts[0] == "effort") {
		level = parts[1]
	} else {
		return "", usagef("invalid effort config key %q (expected effort.<level>.model)", key)
	}
	canonical, ok := engine.NormalizeEffort(level)
	if !ok {
		return "", usagef("unknown effort %q (low|medium|high|max)", level)
	}
	return canonical, nil
}

func validEffort(s string) bool {
	_, ok := engine.NormalizeEffort(s)
	return ok
}
