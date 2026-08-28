package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/engine"
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
	current := "unknown"
	if a.currentVersion != nil {
		current = a.currentVersion()
	}
	fmt.Fprintf(a.stdout, "Current version: %s\n", current)
	fmt.Fprintln(a.stdout, "Checking for updates to latest version...")
	result, err := a.update(ctx)
	if err != nil {
		return err
	}
	a.printUpdateResult(result, inSession)
	if inSession && result.Updated {
		a.armRestart(result.Latest)
	}
	return nil
}

// armRestart records that the session should hand over to the new binary, when
// the user has asked for that. It only records: the exec happens after the
// screen is down.
func (a *app) armRestart(version string) {
	d, err := a.resolve()
	if err != nil {
		return
	}
	cfg, err := config.Load(d.ConfigFile())
	if err != nil || cfg.AutoRestartAfterUpdate == nil || !*cfg.AutoRestartAfterUpdate {
		return
	}
	a.restartInto = version
	fmt.Fprintf(a.stdout, "Restarting into %s — this session continues.\n", version)
}

// restartArgs rebuilds the command line that puts the user back where they
// were: the same session, mode, effort and permission tier. The conversation
// itself is on disk already — sessions save after every step — so resuming by
// id restores the history without kolk having to carry it across the exec.
func restartArgs(ag *engine.Agent) []string {
	args := []string{}
	if ag == nil {
		return args
	}
	if ag.Sess != nil {
		if id := ag.Sess.SessionID(); id != "" {
			args = append(args, "--session", id)
		}
	}
	if ag.Mode != "" {
		args = append(args, "--mode", ag.Mode)
	}
	if ag.Effort != "" {
		args = append(args, "--effort", ag.Effort)
	}
	if ag.Permission != "" {
		args = append(args, "--permission", string(ag.Permission))
	}
	return args
}

// performRestart replaces this process with the updated binary. It runs only
// after the terminal has been restored, and any failure is reported rather
// than swallowed: a restart that silently did not happen would leave the user
// on the old version believing they were on the new one.
func (a *app) performRestart(ag *engine.Agent) {
	if a.restartInto == "" || a.replaceSelf == nil || a.executablePath == nil {
		return
	}
	path, err := a.executablePath()
	if err != nil {
		fmt.Fprintf(a.stderr, "could not restart into %s: %v\nRun kolk again to use it.\n", a.restartInto, err)
		return
	}
	if err := a.replaceSelf(path, restartArgs(ag), os.Environ()); err != nil {
		fmt.Fprintf(a.stderr, "could not restart into %s: %v\nRun kolk again to use it.\n", a.restartInto, err)
	}
}

func (a *app) printUpdateResult(result selfupdate.Result, inSession bool) {
	if result.Updated {
		fmt.Fprintf(a.stdout, "Kolk updated successfully (%s → %s)\nInstalled to: %s\n", result.Current, result.Latest, result.Path)
		if inSession {
			fmt.Fprintf(a.stdout, "Restart kolk to use %s\n", result.Latest)
		}
	} else if result.Current == result.Latest {
		fmt.Fprintf(a.stdout, "Kolk is up to date (%s)\n", result.Current)
	} else {
		fmt.Fprintf(a.stdout, "Kolk is newer than the latest release (current %s; latest %s)\n", result.Current, result.Latest)
	}
	if result.Warning != "" {
		fmt.Fprintf(a.stderr, "warning: %s\n", result.Warning)
	}
}
