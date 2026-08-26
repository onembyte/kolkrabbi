package cli

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/onembyte/kolkrabbi/internal/local"
)

// runLocalia reports what this machine could run locally and what Kolkrabbi is
// currently storing for it. It reads only: nothing here downloads, starts, or
// configures anything, because every pull is an explicit user action.
func (a *app) runLocalia(ctx context.Context, args []string) error {
	if len(args) > 0 {
		return usagef("%s", usageLine("localia"))
	}
	dirs, err := a.resolve()
	if err != nil {
		return err
	}
	modelDir := dirs.LocalModelsDir()

	probe := a.probeHardware
	if probe == nil {
		probe = func(ctx context.Context, dir string) local.Hardware {
			return local.NewSystemProber(dir).Probe(ctx)
		}
	}
	hardware := probe(ctx, modelDir)

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
