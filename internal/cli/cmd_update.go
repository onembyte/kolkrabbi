package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/onembyte/kolkrabbi/internal/selfupdate"
)

func (a *app) runUpdate(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return usagef("%s", usageLine("update"))
	}
	tctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	return a.applyUpdate(tctx, false)
}

func (a *app) applyUpdate(ctx context.Context, inSession bool) error {
	if a.update == nil {
		return fmt.Errorf("updater is not configured")
	}
	result, err := a.update(ctx)
	if err != nil {
		return err
	}
	a.printUpdateResult(result, inSession)
	return nil
}

func (a *app) printUpdateResult(result selfupdate.Result, inSession bool) {
	if result.Updated {
		fmt.Fprintf(a.stdout, "updated kolk %s → %s\npath: %s\n", result.Current, result.Latest, result.Path)
		if inSession {
			fmt.Fprintf(a.stdout, "restart kolk to use %s\n", result.Latest)
		}
	} else {
		fmt.Fprintf(a.stdout, "kolk %s is already current (latest release: %s)\n", result.Current, result.Latest)
	}
	if result.Warning != "" {
		fmt.Fprintf(a.stderr, "warning: %s\n", result.Warning)
	}
}
