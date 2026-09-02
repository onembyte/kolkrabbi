package cli

import (
	"context"
	"fmt"
	"strconv"
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
		return usagef("`/config set-key` was replaced; use `/key <API_KEY>`")
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
		a.printSettings(cfg, "")
		return nil
	}
	// `kolk config <text>` is a search — but only when it finds something. A
	// word that matches no setting is far likelier to be a mistyped subcommand
	// than a search for nothing, and answering "no matches" to `config
	// set-everythign` would hide the typo instead of reporting it.
	if len(args) == 1 && !configVerbs[args[0]] {
		if a.printSettings(cfg, args[0]) {
			return nil
		}
		return usagef("%s", usageLine("config"))
	}

	switch args[0] {
	case "get":
		if len(args) < 2 {
			return usagef("usage: /config get <key>")
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
		case key == "auto_restart_after_update":
			if cfg.AutoRestartAfterUpdate != nil {
				fmt.Fprintln(a.stdout, map[bool]string{true: "on", false: "off"}[*cfg.AutoRestartAfterUpdate])
			} else {
				fmt.Fprintln(a.stdout, "(unset — inherits off)")
			}
		case key == "max_run_cost_usd":
			if cfg.MaxRunCostUSD > 0 {
				fmt.Fprintf(a.stdout, "%.2f\n", cfg.MaxRunCostUSD)
			} else {
				fmt.Fprintln(a.stdout, "(unset — no ceiling)")
			}
		case key == "max_concurrent_tasks":
			if cfg.MaxConcurrentTasks > 0 {
				fmt.Fprintln(a.stdout, cfg.MaxConcurrentTasks)
			} else {
				fmt.Fprintf(a.stdout, "(unset — inherits %d)\n", engine.DefaultConcurrentTasks)
			}
		case key == "subagent_network":
			if cfg.SubagentNetwork != "" {
				fmt.Fprintln(a.stdout, cfg.SubagentNetwork)
			} else {
				fmt.Fprintf(a.stdout, "(unset — inherits %s)\n", engine.SubagentNetworkAuto)
			}
		case strings.HasPrefix(key, "slot."):
			if model := cfg.Slots[strings.TrimPrefix(key, "slot.")]; model != "" {
				fmt.Fprintln(a.stdout, model)
			} else {
				fmt.Fprintln(a.stdout, "(unset — chosen from the catalogue)")
			}
		case key == "routing.on_subscription_limit":
			if cfg.Routing.OnSubscriptionLimit != "" {
				fmt.Fprintln(a.stdout, cfg.Routing.OnSubscriptionLimit)
			} else {
				fmt.Fprintf(a.stdout, "(unset — inherits %s)\n", engine.OnLimitAsk)
			}
		case key == "routing.on_free_exhausted":
			if cfg.Routing.OnFreeExhausted != "" {
				fmt.Fprintln(a.stdout, cfg.Routing.OnFreeExhausted)
			} else {
				fmt.Fprintf(a.stdout, "(unset — inherits %s)\n", engine.OnFreeExhaustedFree)
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
		case strings.HasPrefix(key, "local."):
			value, known := config.GetLocal(cfg, key)
			if !known {
				return usagef("unknown config key %q", key)
			}
			if value == "" {
				fmt.Fprintln(a.stdout, "(unset — Kolkrabbi computes it)")
			} else {
				fmt.Fprintln(a.stdout, value)
			}
		default:
			return usagef("unknown config key %q", key)
		}

	case "set":
		if len(args) < 3 {
			return usagef("usage: /config set <key> <value>")
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
		case key == "auto_restart_after_update":
			on, err := config.ParseOnOff(val)
			if err != nil {
				return usagef("auto_restart_after_update: %v", err)
			}
			cfg.AutoRestartAfterUpdate = &on
			if err := config.Save(d.ConfigFile(), cfg); err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "auto_restart_after_update → %s\n", val)
		case key == "max_run_cost_usd":
			ceiling, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
			if err != nil {
				return usagef("max_run_cost_usd: %q is not a number", val)
			}
			// Zero is meaningful — it is how the ceiling is expressed as "none"
			// in the config — but a negative one is a run that is over budget
			// before it starts.
			if ceiling < 0 {
				return usagef("max_run_cost_usd: %.2f is negative; use 0 for no ceiling", ceiling)
			}
			cfg.MaxRunCostUSD = ceiling
			if err := config.Save(d.ConfigFile(), cfg); err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "max_run_cost_usd → %.2f\n", ceiling)
		case key == "max_concurrent_tasks":
			width, err := strconv.Atoi(strings.TrimSpace(val))
			if err != nil {
				return usagef("max_concurrent_tasks: %q is not a whole number", val)
			}
			// One is sequential, which is a choice. Zero is a run that never
			// starts a task, which is not.
			if width < 1 {
				return usagef("max_concurrent_tasks: %d would run nothing; one is the minimum", width)
			}
			cfg.MaxConcurrentTasks = width
			if err := config.Save(d.ConfigFile(), cfg); err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "max_concurrent_tasks → %d\n", width)
		case key == "subagent_network":
			policy, ok := engine.NormalizeSubagentNetwork(val)
			if !ok {
				return usagef("subagent_network: %q is not a policy; use auto, on, or off", val)
			}
			cfg.SubagentNetwork = policy
			if err := config.Save(d.ConfigFile(), cfg); err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "subagent_network → %s\n", policy)
			if policy == engine.SubagentNetworkOff {
				fmt.Fprintln(a.stdout, "strict: a claude child has no network switch and will be refused; codex children run with network off")
			}
		case strings.HasPrefix(key, "slot."):
			name := strings.TrimPrefix(key, "slot.")
			// Validated at the point of typing. The alternative is a warning at
			// the next session start, which on a setting nobody looks at twice
			// means paying for the wrong model until they happen to notice.
			if err := engine.ValidateSlots(map[string]string{name: val}); err != nil {
				return usagef("%v", err)
			}
			if cfg.Slots == nil {
				cfg.Slots = map[string]string{}
			}
			cfg.Slots[name] = val
			if err := config.Save(d.ConfigFile(), cfg); err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "slot.%s → %s\n", name, val)
		case key == "routing.on_subscription_limit":
			policy, err := engine.NormalizeSubscriptionLimit(val)
			if err != nil {
				return usagef("routing.on_subscription_limit: %v", err)
			}
			cfg.Routing.OnSubscriptionLimit = policy
			if err := config.Save(d.ConfigFile(), cfg); err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "routing.on_subscription_limit → %s\n", policy)
		case key == "routing.on_free_exhausted":
			policy, err := engine.NormalizeFreeExhausted(val)
			if err != nil {
				return usagef("routing.on_free_exhausted: %v", err)
			}
			cfg.Routing.OnFreeExhausted = policy
			if err := config.Save(d.ConfigFile(), cfg); err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "routing.on_free_exhausted → %s\n", policy)
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
		case strings.HasPrefix(key, "local."):
			if err := config.SetLocal(cfg, key, val); err != nil {
				return usagef("%s", err)
			}
			if err := config.Save(d.ConfigFile(), cfg); err != nil {
				return err
			}
			stored, _ := config.GetLocal(cfg, key)
			fmt.Fprintf(a.stdout, "%s → %s\n", key, stored)
		default:
			return usagef("unknown config key %q", key)
		}

	case "unset":
		if len(args) < 2 {
			return usagef("usage: /config unset <key>")
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
		case key == "max_run_cost_usd":
			cfg.MaxRunCostUSD = 0
			if err := config.Save(d.ConfigFile(), cfg); err != nil {
				return err
			}
			fmt.Fprintln(a.stdout, "removed max_run_cost_usd; a run has no cost ceiling")
		case key == "max_concurrent_tasks":
			cfg.MaxConcurrentTasks = 0
			if err := config.Save(d.ConfigFile(), cfg); err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "removed max_concurrent_tasks; back to %d at a time\n",
				engine.DefaultConcurrentTasks)
		case key == "subagent_network":
			cfg.SubagentNetwork = ""
			if err := config.Save(d.ConfigFile(), cfg); err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "removed subagent_network; back to %s\n", engine.SubagentNetworkAuto)
		case strings.HasPrefix(key, "slot."):
			name := strings.TrimPrefix(key, "slot.")
			delete(cfg.Slots, name)
			// An emptied map is nilled so it leaves the file entirely: a
			// `"slots": {}` left behind reads as a decision someone made.
			if len(cfg.Slots) == 0 {
				cfg.Slots = nil
			}
			if err := config.Save(d.ConfigFile(), cfg); err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "removed slot.%s\n", name)
		case key == "routing.on_subscription_limit":
			cfg.Routing.OnSubscriptionLimit = ""
			if err := config.Save(d.ConfigFile(), cfg); err != nil {
				return err
			}
			fmt.Fprintln(a.stdout, "removed routing.on_subscription_limit; back to asking")
		case key == "routing.on_free_exhausted":
			cfg.Routing.OnFreeExhausted = ""
			if err := config.Save(d.ConfigFile(), cfg); err != nil {
				return err
			}
			fmt.Fprintln(a.stdout, "removed routing.on_free_exhausted; back to staying free")
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
		case strings.HasPrefix(key, "local."):
			if err := config.UnsetLocal(cfg, key); err != nil {
				return usagef("%s", err)
			}
			if err := config.Save(d.ConfigFile(), cfg); err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "removed %s\n", key)
		default:
			return usagef("unknown config key %q", key)
		}

	case "set-model":
		if len(args) < 2 {
			return usagef("usage: /config set-model <model>")
		}
		cfg.Model = strings.Join(args[1:], " ")
		if err := config.Save(d.ConfigFile(), cfg); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "default model set to %s\n", cfg.Model)

	case "set-base-url":
		if len(args) < 2 {
			return usagef("usage: /config set-base-url <url>")
		}
		cfg.BaseURL = strings.TrimRight(args[1], "/")
		if err := config.Save(d.ConfigFile(), cfg); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "base URL set to %s\n", cfg.BaseURL)
		if !provider.IsOpenRouterEndpoint(cfg.BaseURL) {
			// Said at the moment of choosing, because it is the one moment the
			// user is thinking about this endpoint: the OpenRouter key is bound
			// to openrouter.ai and will not be sent here.
			fmt.Fprintln(a.stdout, "this endpoint is used without a key; the OpenRouter key only ever goes to openrouter.ai")
		}

	case "set-tier":
		if len(args) < 3 {
			return usagef("usage: /config set-tier <low|medium|high|max> <model>")
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
			fmt.Fprintln(a.stdout, "tiers:    (none — all efforts use the session model; set with `/config set-tier`)")
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

// configVerbs are the words that are commands rather than search text, so
// `kolk config get` cannot be read as a search for "get".
var configVerbs = map[string]bool{
	"get": true, "set": true, "unset": true, "path": true, "edit": true, "show": true, "list": true,
}

// printSettings renders the settings table, optionally filtered. Every row
// shows the value in effect, with unset rows marked, because the question a
// person opens this to answer is "what is kolk doing", not "what did I type".
func (a *app) printSettings(cfg *config.Config, filter string) bool {
	rows := cfg.Settings(defaultModel, provider.DefaultBaseURL)
	filter = strings.ToLower(strings.TrimSpace(filter))
	if filter != "" {
		kept := rows[:0]
		for _, row := range rows {
			if strings.Contains(strings.ToLower(row.Key), filter) ||
				strings.Contains(strings.ToLower(row.Summary), filter) ||
				strings.Contains(strings.ToLower(row.Value), filter) {
				kept = append(kept, row)
			}
		}
		rows = kept
	}
	if len(rows) == 0 {
		return false
	}
	width := 0
	for _, row := range rows {
		if n := len(row.Key); n > width {
			width = n
		}
	}
	for _, row := range rows {
		value := row.Value
		if row.Default {
			value += "  (default)"
		}
		fmt.Fprintf(a.stdout, "%-*s  %s\n", width, row.Key, value)
	}
	fmt.Fprintf(a.stdout, "\n%d settings · /config <text> to search · /config set <key> <value>\n", len(rows))
	return true
}
