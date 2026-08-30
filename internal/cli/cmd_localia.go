package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/local"
)

// runLocalia reports what this machine could run locally and what Kolkrabbi is
// currently storing for it. It reads only: nothing here downloads, starts, or
// configures anything, because every pull is an explicit user action.
func (a *app) runLocalia(ctx context.Context, args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "models":
			return a.printLocalCatalog(strings.Join(args[1:], " "))
		case "plan":
			if len(args) < 2 {
				return usagef("usage: kolk localia plan <model>")
			}
			return a.printLocalPlan(ctx, args[1])
		case "pull":
			rest, approved := stripYesFlag(args[1:])
			if len(rest) < 1 {
				return usagef("usage: kolk localia pull [--yes] <model>")
			}
			return a.pullLocalModel(ctx, rest[0], approved)
		default:
			return usagef("%s", usageLine("localia"))
		}
	}
	dirs, err := a.resolve()
	if err != nil {
		return err
	}
	// The store is the user's own Ollama's (option E): OLLAMA_MODELS or
	// ~/.ollama/models. Free disk is measured where the pull will land, on
	// the nearest directory of that path that exists.
	store := local.HostModelDir(os.Environ())
	hardware := a.hardware(ctx, existingAncestor(store))

	fmt.Fprintf(a.stdout, "system RAM: %s\n", capacityLabel(hardware.SystemRAM))
	fmt.Fprintf(a.stdout, "free disk:  %s\n", capacityLabel(hardware.DiskFree))
	fmt.Fprintf(a.stdout, "model store: %s (your Ollama's)\n", store)

	fmt.Fprintln(a.stdout, "\nACCELERATORS")
	if len(hardware.Accelerators) == 0 {
		fmt.Fprintln(a.stdout, "  none detected — models would run on the CPU")
	}
	for _, card := range hardware.Accelerators {
		fmt.Fprintf(a.stdout, "  %-8s %-10s vram %-12s available %s\n",
			card.Vendor, card.Name, capacityLabel(card.VRAM), capacityLabel(card.AvailableVRAM))
	}

	cfg, err := config.Load(dirs.ConfigFile())
	if err != nil {
		return err
	}
	fmt.Fprintln(a.stdout, "\nSETTINGS")
	for _, key := range config.LocalKeys {
		value, _ := config.GetLocal(cfg, key)
		if value == "" {
			value = "(computed)"
		} else if key == "local.reserved_ram_bytes" {
			// Everything else on this screen is in GiB; a raw byte count here
			// would be the one number the reader has to convert themselves.
			if bytes, err := strconv.ParseUint(value, 10, 64); err == nil {
				value = local.HumanBytes(bytes)
			}
		}
		fmt.Fprintf(a.stdout, "  %-30s %s\n", key, value)
	}
	fmt.Fprintf(a.stdout, "  change with: kolk config set %s <value>\n", config.LocalKeys[0])

	// What is pulled, by the record that exists: a running server's own list,
	// else the manifest tree the last pull left in the store.
	fmt.Fprintln(a.stdout, "\nPULLED")
	pulled := a.pulledModelNames(ctx)
	if len(pulled) == 0 {
		fmt.Fprintln(a.stdout, "  nothing pulled yet; Kolkrabbi never pulls one on its own — `kolk localia pull <model>` asks first")
		return nil
	}
	for _, name := range pulled {
		fmt.Fprintf(a.stdout, "  %s\n", name)
	}
	return nil
}

// pulledModelNames lists the host's pulled models: the server's own answer
// when one runs, else the libraries the manifest tree records.
func (a *app) pulledModelNames(ctx context.Context) []string {
	var names []string
	if a.discoverHost != nil && a.listHostModels != nil {
		if host := a.discoverHost(ctx); host.State == local.HostRunning {
			if models, err := a.listHostModels(ctx, host.Addr, ""); err == nil {
				for _, m := range models {
					names = append(names, m.Name)
				}
				sort.Strings(names)
				return names
			}
		}
	}
	if a.pulledNames != nil {
		for name := range a.pulledNames() {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// existingAncestor is the nearest directory of path that exists, so free disk
// can be measured for a store that has not been created yet.
func existingAncestor(path string) string {
	for p := path; p != "" && p != "."; p = filepath.Dir(p) {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return p
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	return path
}

// capacityLabel keeps "unknown" visibly different from a measured value. A
// probe that could not read a number must never be shown as 0 B, which reads
// as a fact about the machine.
func capacityLabel(c local.Capacity) string {
	if !c.Known {
		return "unknown"
	}
	return local.HumanBytes(c.Bytes)
}

// printLocalCatalog lists what Kolkrabbi knows how to plan for. Runtime figures
// are estimates and say so: the exact need depends on context length, batch
// size and the runtime's own overhead, none of which Kolkrabbi controls.
func (a *app) printLocalCatalog(filter string) error {
	entries := local.Catalog(filter)
	if len(entries) == 0 {
		fmt.Fprintf(a.stdout, "no local model matches %q\n", filter)
		return nil
	}
	fmt.Fprintln(a.stdout, "MODEL                  PARAMS  QUANT      DOWNLOAD    NEEDS (estimate)")
	for _, entry := range entries {
		requirement := entry.Requirement()
		fmt.Fprintf(a.stdout, "%-22s %-7s %-10s %-11s %s on gpu / %s on cpu\n",
			entry.Name, entry.Parameters, entry.Quantization,
			local.HumanBytes(entry.StorageBytes),
			local.HumanBytes(requirement.VRAMBytes), local.HumanBytes(requirement.RAMBytes))
	}
	fmt.Fprintln(a.stdout, "\nplan one before pulling it: kolk localia plan <model>")
	return nil
}

// printLocalPlan shows where a model would run and every number that decision
// rested on. It downloads nothing: seeing the plan must never be the act that
// commits a multi-gigabyte pull.
func (a *app) printLocalPlan(ctx context.Context, name string) error {
	entry, err := local.LookupModel(name)
	if err != nil {
		return err
	}
	dirs, err := a.resolve()
	if err != nil {
		return err
	}
	cfg, err := config.Load(dirs.ConfigFile())
	if err != nil {
		return err
	}
	hardware := a.hardware(ctx, existingAncestor(local.HostModelDir(os.Environ())))

	plan, err := local.PlanFit(hardware, localRuntimeConfig(cfg), entry.Requirement())
	if err != nil {
		return err
	}

	fmt.Fprintf(a.stdout, "%s (%s, %s)\n", entry.Name, entry.Parameters, entry.Quantization)
	fmt.Fprintf(a.stdout, "  download:  %s into your Ollama's store, %s\n", local.HumanBytes(plan.StorageBytes), local.HostModelDir(os.Environ()))
	fmt.Fprintf(a.stdout, "  disk free: %s\n", local.HumanBytes(plan.DiskFreeBytes))
	fmt.Fprintf(a.stdout, "  placement: %s", plan.Placement)
	if plan.Accelerator != "" {
		fmt.Fprintf(a.stdout, " (%s, index %d)", plan.Accelerator, plan.GPUIndex)
	}
	fmt.Fprintln(a.stdout)
	fmt.Fprintf(a.stdout, "  needs:     %s (estimate)\n", local.HumanBytes(plan.RequiredBytes))
	fmt.Fprintf(a.stdout, "  available: %s after %s reserved\n",
		local.HumanBytes(plan.AvailableBytes), local.HumanBytes(plan.ReservedBytes))
	if plan.Fallback != "" {
		fmt.Fprintf(a.stdout, "  fallback:  %s\n", plan.Fallback)
	}
	fmt.Fprintln(a.stdout, "\nnothing has been downloaded; this is a plan, not a pull")
	return nil
}

// hardwareProbeTimeout bounds the whole snapshot. A vendor tool that hangs —
// nvidia-smi against a wedged driver is the known case — must not hang a
// session, and "unknown" is a valid answer everywhere the snapshot is used.
const hardwareProbeTimeout = 5 * time.Second

// hardware probes this machine, or uses whatever a test injected.
func (a *app) hardware(ctx context.Context, modelDir string) local.Hardware {
	bounded, cancel := context.WithTimeout(ctx, hardwareProbeTimeout)
	defer cancel()
	if a.probeHardware != nil {
		return a.probeHardware(bounded, modelDir)
	}
	return local.NewSystemProber(modelDir).Probe(bounded)
}

// localRuntimeConfig turns saved settings into the planner's input, leaving
// anything unset to the planner's own defaults.
func localRuntimeConfig(cfg *config.Config) local.Config {
	runtime := local.Config{
		GPUMode:      cfg.Local.GPUMode,
		Quantization: cfg.Local.Quantization,
	}
	if cfg.Local.GPUIndex != nil {
		runtime.GPUIndex = *cfg.Local.GPUIndex
	}
	// Unset means "use the documented default headroom", not "reserve nothing".
	// A user who chose zero gets zero; a user who chose nothing gets protected.
	runtime.ReservedVRAMFraction = local.DefaultReservedVRAMFraction
	if cfg.Local.ReservedVRAMFraction != nil {
		runtime.ReservedVRAMFraction = *cfg.Local.ReservedVRAMFraction
	}
	runtime.ReservedRAMBytes = local.DefaultReservedRAM
	if cfg.Local.ReservedRAMBytes != nil {
		runtime.ReservedRAMBytes = *cfg.Local.ReservedRAMBytes
	}
	return runtime
}

// stripYesFlag pulls the non-interactive approval out of the arguments.
func stripYesFlag(args []string) ([]string, bool) {
	rest, approved := make([]string, 0, len(args)), false
	for _, arg := range args {
		if arg == "--yes" || arg == "-y" {
			approved = true
			continue
		}
		rest = append(rest, arg)
	}
	return rest, approved
}

// pullLocalModel plans, asks, and only then installs. The order matters: a
// model that cannot fit is refused before the user is asked to approve a
// download that could never have worked.
func (a *app) pullLocalModel(ctx context.Context, name string, approved bool) error {
	entry, err := local.LookupModel(name)
	if err != nil {
		return err
	}
	dirs, err := a.resolve()
	if err != nil {
		return err
	}
	cfg, err := config.Load(dirs.ConfigFile())
	if err != nil {
		return err
	}
	plan, err := local.PlanFit(a.hardware(ctx, existingAncestor(local.HostModelDir(os.Environ()))), localRuntimeConfig(cfg), entry.Requirement())
	if err != nil {
		return err
	}

	fmt.Fprintf(a.stdout, "%s (%s, %s)\n", entry.Name, entry.Parameters, entry.Quantization)
	fmt.Fprintf(a.stdout, "  download:  %s into your Ollama's store, %s\n", local.HumanBytes(plan.StorageBytes), local.HostModelDir(os.Environ()))
	fmt.Fprintf(a.stdout, "  placement: %s", plan.Placement)
	if plan.Accelerator != "" {
		fmt.Fprintf(a.stdout, " (%s, index %d)", plan.Accelerator, plan.GPUIndex)
	}
	fmt.Fprintln(a.stdout)
	fmt.Fprintf(a.stdout, "  needs:     %s of %s available (estimate)\n",
		local.HumanBytes(plan.RequiredBytes), local.HumanBytes(plan.AvailableBytes))
	if plan.Fallback != "" {
		fmt.Fprintf(a.stdout, "  fallback:  %s\n", plan.Fallback)
	}

	if !approved {
		// A session reads the keyboard from its own goroutine, so prompting
		// here would compete with it for the user's keystrokes — the same
		// contention a provider login would cause.
		if a.terminalOwned != nil && a.terminalOwned() {
			return fmt.Errorf("a pull needs a yes or no, which this session cannot ask for; run `kolk localia pull %s` in another terminal, or repeat it here with --yes", entry.Name)
		}
		if !a.confirmed("Download and install it now?") {
			fmt.Fprintln(a.stdout, "cancelled; nothing was downloaded")
			return nil
		}
	}

	// The pull is the host's own (E10): the bytes land in its store, and
	// `ollama list` shows them afterwards exactly as if the user had typed
	// `ollama pull` — which is what this is, with the fit plan and the
	// approval above in front of it.
	host := a.discoverHost(ctx)
	addr := host.Addr
	switch host.State {
	case local.HostAbsent:
		return fmt.Errorf("ollama is not installed, so nothing can pull %s; install it with: %s", entry.Name, host.InstallHint())
	case local.HostInstalled:
		// Installed and idle: a pull is exactly the first use that earns a
		// started server. Stopped again when the pull is done.
		started, stop, err := a.startHostFor(ctx, host)
		if err != nil {
			return err
		}
		defer stop()
		addr = started
	}
	fmt.Fprintf(a.stdout, "pulling %s through ollama at %s\n", entry.Name, addr)
	if err := local.PullHostModel(ctx, addr, entry.Name, a.stdout); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "%s is pulled; `/model` lists it as ollama/%s\n", entry.Name, entry.Name)
	return nil
}

// startHostFor brings up the user's idle Ollama for one command, through the
// same starter a session uses, and hands back the way to stop it.
func (a *app) startHostFor(ctx context.Context, host local.Host) (string, func(), error) {
	if a.startHost != nil {
		return a.startHost(ctx, host)
	}
	starter := &local.HostStarter{Binary: host.Binary, Environ: os.Environ(), Out: a.stdout}
	addr, err := starter.Ensure(ctx)
	if err != nil {
		return "", func() {}, err
	}
	return addr, func() { _ = starter.Close() }, nil
}

// confirmed asks one yes-or-no question. Anything that is not an explicit yes
// is a no, including end of input: a closed stdin must never approve a
// multi-gigabyte download.
func (a *app) confirmed(question string) bool {
	fmt.Fprintf(a.stdout, "\n%s [y/N] ", question)
	if a.in == nil {
		return false
	}
	line, err := a.in.ReadString('\n')
	if err != nil && line == "" {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}
