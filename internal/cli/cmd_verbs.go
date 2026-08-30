package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

func (a *app) runModel(ctx context.Context, args []string) error {
	if len(args) == 0 {
		if err := a.printPlanModelChoices(); err != nil {
			return err
		}
		return a.runModels(ctx, nil)
	}

	d, err := a.resolve()
	if err != nil {
		return err
	}

	cfg, err := config.Load(d.ConfigFile())
	if err != nil {
		return err
	}

	resolved := provider.ResolveModelAlias(strings.Join(args, " "))
	cfg.Model = resolved

	if err := config.Save(d.ConfigFile(), cfg); err != nil {
		return err
	}

	fmt.Fprintf(a.stdout, "default model set to %s\n", resolved)
	return nil
}

func (a *app) runEffort(_ context.Context, args []string) error {
	d, err := a.resolve()
	if err != nil {
		return err
	}

	cfg, err := config.Load(d.ConfigFile())
	if err != nil {
		return err
	}

	if len(args) == 0 {
		effort := engine.EffortMedium
		if cfg.Effort != "" {
			effort = cfg.Effort
		}
		fmt.Fprintf(a.stdout, "default effort: %s\n", effort)
		return nil
	}

	canonical, ok := engine.NormalizeEffort(args[0])
	if !ok {
		return usagef("unknown effort %q (low|medium|high|max)", args[0])
	}

	cfg.Effort = canonical

	if err := config.Save(d.ConfigFile(), cfg); err != nil {
		return err
	}

	fmt.Fprintf(a.stdout, "default effort set to %s\n", canonical)
	return nil
}

func (a *app) runMode(_ context.Context, args []string) error {
	d, err := a.resolve()
	if err != nil {
		return err
	}

	cfg, err := config.Load(d.ConfigFile())
	if err != nil {
		return err
	}

	if len(args) == 0 {
		mode := engine.ModeCode
		if cfg.Mode != "" {
			mode = cfg.Mode
		}
		fmt.Fprintf(a.stdout, "mode: %s\n", mode)
		return nil
	}

	mode := strings.ToLower(args[0])
	valid := false
	for _, m := range engine.Modes {
		if mode == m {
			valid = true
			break
		}
	}
	if !valid {
		return usagef("unknown mode %q (chat|code|agent)", args[0])
	}

	cfg.Mode = mode

	if err := config.Save(d.ConfigFile(), cfg); err != nil {
		return err
	}

	fmt.Fprintf(a.stdout, "mode set to %s\n", mode)
	return nil
}
