package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

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
	modelDir := dirs.LocalModelsDir()

	hardware := a.hardware(ctx, modelDir)

	fmt.Fprintf(a.stdout, "system RAM: %s\n", capacityLabel(hardware.SystemRAM))
	fmt.Fprintf(a.stdout, "free disk:  %s\n", capacityLabel(hardware.DiskFree))
	fmt.Fprintf(a.stdout, "model dir:  %s\n", modelDir)

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

	installed, err := installedLocalModels(modelDir)
	if err != nil {
		return err
	}
	fmt.Fprintln(a.stdout, "\nINSTALLED")
	if len(installed) == 0 {
		fmt.Fprintln(a.stdout, "  no local model is installed; Kolkrabbi never pulls one on its own")
		return nil
	}
	for _, name := range installed {
		fmt.Fprintf(a.stdout, "  %s\n", name)
	}
	return nil
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

func installedLocalModels(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("localia: read the managed model directory: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
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
	hardware := a.hardware(ctx, dirs.LocalModelsDir())

	plan, err := local.PlanFit(hardware, localRuntimeConfig(cfg), entry.Requirement())
	if err != nil {
		return err
	}

	fmt.Fprintf(a.stdout, "%s (%s, %s)\n", entry.Name, entry.Parameters, entry.Quantization)
	fmt.Fprintf(a.stdout, "  download:  %s into %s\n", local.HumanBytes(plan.StorageBytes), dirs.LocalModelsDir())
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

// hardware probes this machine, or uses whatever a test injected.
func (a *app) hardware(ctx context.Context, modelDir string) local.Hardware {
	if a.probeHardware != nil {
		return a.probeHardware(ctx, modelDir)
	}
	return local.NewSystemProber(modelDir).Probe(ctx)
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
	plan, err := local.PlanFit(a.hardware(ctx, dirs.LocalModelsDir()), localRuntimeConfig(cfg), entry.Requirement())
	if err != nil {
		return err
	}

	fmt.Fprintf(a.stdout, "%s (%s, %s)\n", entry.Name, entry.Parameters, entry.Quantization)
	fmt.Fprintf(a.stdout, "  download:  %s into %s\n", local.HumanBytes(plan.StorageBytes), dirs.LocalModelsDir())
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

	if !approved && !a.confirmed("Download and install it now?") {
		fmt.Fprintln(a.stdout, "cancelled; nothing was downloaded")
		return nil
	}

	runtime := filepath.Join(dirs.LocalModelsDir(), "runtime", local.SidecarName)
	if _, err := os.Stat(runtime); err != nil {
		// Kolkrabbi runs its own sidecar and never touches a host installation,
		// so there is nothing to pull through until that runtime is installed.
		// Installing it is its own approved step, not a side effect of this one.
		return fmt.Errorf("the managed local runtime is not installed at %s, so %s cannot be pulled yet; installing it is a separate approved step",
			runtime, entry.Name)
	}
	return fmt.Errorf("the managed local runtime at %s cannot pull models yet", runtime)
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
