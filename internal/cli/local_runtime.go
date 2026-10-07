package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/local"
	"github.com/onembyte/kolkrabbi/internal/paths"
)

// localInstaller is every native runtime installer this surface builds, so
// discovery, setup and housekeeping read the same machine and the same
// choice: CPU chosen (local.gpu_mode cpu) needs no accelerator bundle.
func (a *app) localInstaller(dir string, progress func(local.RuntimeProgress)) *local.RuntimeInstaller {
	return &local.RuntimeInstaller{Dir: dir, Progress: progress, Hardware: a.localHardware, CPUOnly: a.localCPUChosen()}
}

// localCPUChosen is the user's own choice of CPU for local models, read when
// asked so a /config change applies at once. An unreadable config is no
// choice: the automatic one stands.
func (a *app) localCPUChosen() bool {
	d, err := a.locate()
	if err != nil {
		return false
	}
	cfg, err := config.Load(d.ConfigFile())
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(cfg.Local.GPUMode), "cpu")
}

func (a *app) bindLocalRuntime(cfg *config.Config, dirs paths.Dirs, root string) {
	if a.discoverHost == nil {
		return
	}
	a.localRuntime = &local.HostStarter{
		Discover: func(ctx context.Context) local.Host { return a.discoverLocalRuntime(ctx, dirs) }, Environ: os.Environ(), Out: a.stdout,
		Persistent: !cfg.LocalForProject(root).EphemeralEnabled(),
		CPUChosen:  a.localCPUChosen,
		Project:    root, StateDir: filepath.Join(dirs.LocalRuntimeDir(), "projects"),
	}
	if a.installLocalRuntime != nil {
		a.localRuntime.Provision = func(ctx context.Context, out io.Writer) (local.Host, error) {
			return a.provisionLocalRuntime(ctx, dirs, out)
		}
	}
	// What a killed setup left behind goes on the next start, whether or not
	// a setup follows; a setup in progress is left alone.
	a.localRuntime.Tidy = func(out io.Writer) {
		a.localInstaller(filepath.Join(dirs.LocalRuntimeDir(), "installations"), func(p local.RuntimeProgress) {
			if p.Stage == "note" {
				fmt.Fprintf(out, "  ! %s\n", p.Note)
			}
		}).SweepStale()
	}
}

func (a *app) localHost(ctx context.Context) local.Host {
	if a.localRuntime != nil {
		return a.localRuntime.Host(ctx)
	}
	if a.discoverHost != nil {
		if dirs, err := a.locate(); err == nil {
			return a.discoverLocalRuntime(ctx, dirs)
		}
		return a.discoverHost(ctx)
	}
	return local.Host{}
}

func (a *app) stopLocalRuntime(ctx context.Context) error {
	h := a.localRuntime
	if h == nil {
		dirs, err := a.locate()
		if err != nil {
			return err
		}
		root, err := verifiedProjectRoot()
		if err != nil {
			return err
		}
		h = &local.HostStarter{Project: root, StateDir: filepath.Join(dirs.LocalRuntimeDir(), "projects")}
	}
	if err := h.StopKept(ctx); err != nil {
		return err
	}
	fmt.Fprintln(a.stdout, "✓ stopped kept Localia runtime for this project")
	return nil
}

func (a *app) discoverLocalRuntime(ctx context.Context, dirs paths.Dirs) local.Host {
	host := a.discoverHost(ctx)
	if host.State != local.HostAbsent {
		return host
	}
	if installed, err := a.localInstaller(filepath.Join(dirs.LocalRuntimeDir(), "installations"), nil).Find(); err == nil {
		return installed
	}
	// Ensure reports a damaged cache explicitly before attempting installation.
	return host
}

func (a *app) provisionLocalRuntime(ctx context.Context, dirs paths.Dirs, out io.Writer) (local.Host, error) {
	if a.installLocalRuntime == nil {
		return local.Host{}, fmt.Errorf("native runtime setup is unavailable in this session")
	}
	if out == nil {
		out = io.Discard
	}
	lastStage, lastBundle, lastPercent := "", "", -10
	progress := func(p local.RuntimeProgress) {
		// Each bundle is its own download; a companion starts its own count
		// rather than hiding behind the standard bundle's 100%.
		if p.Stage == "downloading" && p.Bundle != lastBundle {
			lastStage, lastBundle, lastPercent = "", p.Bundle, -10
		}
		switch p.Stage {
		case "waiting":
			fmt.Fprintln(out, "• Preparing Localia")
		case "checking":
			fmt.Fprintln(out, "  └ Finding the current official Ollama release")
		case "downloading":
			percent := int64(0)
			if p.Total > 0 {
				percent = p.Completed * 100 / p.Total
			}
			if lastStage == p.Stage && int(percent) < lastPercent+10 && percent != 100 {
				return
			}
			if lastStage == p.Stage && int(percent) == lastPercent {
				return
			}
			fmt.Fprintf(out, "  └ %s %d%% · %s / %s\n", local.RuntimeBundleLabel(p.Bundle), percent, local.HumanBytes(uint64(max(p.Completed, 0))), local.HumanBytes(uint64(max(p.Total, 0))))
			lastPercent = int(percent)
		case "extracting":
			fmt.Fprintln(out, "  └ Checksum verified · installing native runtime")
		case "note":
			fmt.Fprintf(out, "  ! %s\n", p.Note)
		case "ready":
			fmt.Fprintf(out, "✓ Localia runtime ready · Ollama %s\n", p.Version)
		}
		lastStage = p.Stage
	}
	return a.installLocalRuntime(ctx, filepath.Join(dirs.LocalRuntimeDir(), "installations"), progress)
}

func (a *app) setupLocalRuntime(ctx context.Context) error {
	// An explicit setup is the retry a remembered failure withholds: forget
	// those first, so the next start tries each release again.
	if dirs, err := a.locate(); err == nil {
		if err := a.localInstaller(filepath.Join(dirs.LocalRuntimeDir(), "installations"), nil).ForgetFailures(ctx); err != nil {
			return err
		}
		if a.localRuntime != nil {
			a.localRuntime.ClearCompanionFailure()
		}
	}
	host := a.localHost(ctx)
	if host.State == local.HostRunning {
		// A running runtime is reused as it is; say what it still lacks.
		fmt.Fprintf(a.stdout, "✓ Localia is ready at %s\n", host.Addr)
		a.printAcceleratorStatus(host)
		return nil
	}
	addr, stop, err := a.startHostFor(ctx, host)
	if err != nil {
		return err
	}
	defer stop()
	fmt.Fprintf(a.stdout, "✓ Localia is ready at %s · choose a cached model with /model, or download one with /localia pull <model>\n", addr)
	return nil
}
