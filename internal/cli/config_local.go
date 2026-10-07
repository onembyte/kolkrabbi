package cli

import (
	"context"
	"fmt"

	"github.com/onembyte/kolkrabbi/internal/config"
)

// This preference applies to the verified project, so a setting typed from a
// subdirectory or through a symlink has the same scope as the agent's tools.
func (a *app) configLocalLifetime(ctx context.Context, cfg *config.Config, file string, args []string) error {
	root, err := verifiedProjectRoot()
	if err != nil {
		return err
	}
	wasOff := !cfg.LocalForProject(root).EphemeralEnabled()
	switch args[0] {
	case "get":
	case "set":
		if len(args) != 3 {
			return usagef("usage: /config set local.ephemeral <on|off>")
		}
		on, err := config.ParseOnOff(args[2])
		if err != nil {
			return usagef("local.ephemeral: %v", err)
		}
		if cfg.Local.ProjectEphemeral == nil {
			cfg.Local.ProjectEphemeral = map[string]bool{}
		}
		cfg.Local.ProjectEphemeral[root] = on
		if err := config.Save(file, cfg); err != nil {
			return err
		}
	case "unset":
		delete(cfg.Local.ProjectEphemeral, root)
		if err := config.Save(file, cfg); err != nil {
			return err
		}
	default:
		return usagef("use /config get, set or unset local.ephemeral")
	}
	value := "on"
	if !cfg.LocalForProject(root).EphemeralEnabled() {
		value = "off"
	}
	fmt.Fprintf(a.stdout, "%s · local.ephemeral · project %s\n", value, root)
	if args[0] != "get" {
		fmt.Fprintln(a.stdout, "Applies to the next session. On stops Kolk's runtime at session close; off keeps it for this project. Downloaded models stay cached.")
		if wasOff && value == "on" && a.localRuntime != nil {
			if addr := a.localRuntime.RecordedRuntime(ctx); addr != "" {
				fmt.Fprintf(a.stdout, "Kolk's runtime is still running at %s; use /localia stop when you want to stop it.\n", addr)
			}
		}
	}
	return nil
}
