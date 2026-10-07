package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/local"
)

// runLocalia reports local capacity and cached models. Status, models and plan
// are read-only; explicit setup, pull and endpoint commands perform their action.
func (a *app) runLocalia(ctx context.Context, args []string) error {
	return a.runLocaliaWith(ctx, nil, args)
}

// runLocaliaWith is runLocalia with the running session, when there is one:
// `use` switches the model in place rather than telling the user to.
func (a *app) runLocaliaWith(ctx context.Context, ag *engine.Agent, args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "stop":
			if len(args) != 1 {
				return usagef("usage: /localia stop")
			}
			return a.stopLocalRuntime(ctx)
		case "setup":
			if len(args) != 1 {
				return usagef("usage: /localia setup")
			}
			return a.setupLocalRuntime(ctx)
		case "add":
			if len(args) < 3 {
				return usagef("usage: /localia add <name> <host:port>")
			}
			return a.addEndpoint(ctx, args[1], args[2])
		case "rm":
			if len(args) < 2 {
				return usagef("usage: /localia rm <name>")
			}
			return a.removeEndpoint(args[1])
		case "list":
			return a.listEndpoints()
		case "direct":
			rest, approved := stripYesFlag(args[1:])
			return a.directRun(ctx, ag, rest, approved)
		case "use":
			if len(args) < 2 {
				return usagef("usage: /localia use <name> [model]")
			}
			return a.useEndpoint(ctx, ag, args[1], strings.Join(args[2:], " "))
		case "models":
			return a.printLocalCatalog(strings.Join(args[1:], " "))
		case "plan":
			if len(args) < 2 {
				return usagef("usage: /localia plan <model>")
			}
			return a.printLocalPlan(ctx, args[1])
		case "pull":
			rest, approved := stripYesFlag(args[1:])
			if len(rest) < 1 {
				return usagef("usage: /localia pull [--yes] <model>")
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
	if root, err := verifiedProjectRoot(); err == nil {
		cfg.Local = cfg.LocalForProject(root)
	}
	fmt.Fprintln(a.stdout, "\nSETTINGS")
	for _, key := range config.LocalKeys {
		value, _ := config.GetLocal(cfg, key)
		if key == "local.ephemeral" && value == "" {
			value = "on (default; stops at session close)"
		}
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
	fmt.Fprintf(a.stdout, "  change with: /config set %s <value>\n", config.LocalKeys[0])
	if a.localRuntime != nil {
		fmt.Fprintln(a.stdout, "\nRUNTIME")
		host := a.localHost(ctx)
		switch {
		case host.State == local.HostRunning && !host.Managed:
			fmt.Fprintf(a.stdout, "  %s · user managed; Kolk leaves it running\n", host.Addr)
		case host.KeptRunning:
			fmt.Fprintf(a.stdout, "  %s · kept running from an earlier `local.ephemeral off`; this session reuses it and leaves it running\n", host.Addr)
			if a.localRuntime.RecordedRuntime(ctx) == host.Addr {
				fmt.Fprintln(a.stdout, "  stop it explicitly with /localia stop")
			}
		case a.localRuntime.Persistent:
			fmt.Fprintf(a.stdout, "  %s · stays running for this project after session close\n", host.State)
		default:
			fmt.Fprintf(a.stdout, "  %s · stops when this session closes\n", host.State)
		}
		fmt.Fprintln(a.stdout, "  downloaded models stay cached; config changes apply next session")
		a.printStalledRuntime(host)
		a.printAcceleratorStatus(host)
	}

	// What is pulled, by the record that exists: a running server's own list,
	// else the manifest tree the last pull left in the store.
	fmt.Fprintln(a.stdout, "\nPULLED")
	pulled := a.pulledModelNames(ctx)
	if len(pulled) == 0 {
		fmt.Fprintln(a.stdout, "  nothing pulled yet; Kolkrabbi never pulls one on its own — `/localia pull <model>` asks first")
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
		if host := a.localHost(ctx); host.State == local.HostRunning {
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
	fmt.Fprintln(a.stdout, "\nplan one before pulling it: /localia plan <model>")
	fmt.Fprintln(a.stdout, "Ollama Cloud tags run on ollama.com and need no plan: /localia pull --yes <tag>-cloud")
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
	hardware := a.usableHardware(ctx, existingAncestor(local.HostModelDir(os.Environ())))

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

// usableHardware is the hardware a fit plan may place a model on: the probe,
// less accelerators the local runtime cannot use (an AMD GPU with no ROCm
// bundle for it), so a plan never says "gpu" beside a note saying "CPU".
func (a *app) usableHardware(ctx context.Context, modelDir string) local.Hardware {
	hardware := a.hardware(ctx, modelDir)
	unusable := a.localHost(ctx).UnusableVendor()
	if unusable == "" {
		return hardware
	}
	usable := make([]local.Accelerator, 0, len(hardware.Accelerators))
	for _, card := range hardware.Accelerators {
		if card.Vendor != unusable {
			usable = append(usable, card)
		}
	}
	hardware.Accelerators = usable
	return hardware
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
	// A picker id pasted whole names the same model: ollama/<name> is the
	// route, and the server knows only <name>.
	name = strings.TrimPrefix(strings.TrimSpace(name), local.HostPrefix)
	entry, err := local.LookupModel(name)
	if err != nil {
		// A Cloud tag is outside the fit catalog: its weights stay on
		// ollama.com, and its pull fetches only the manifest.
		if local.IsCloudModelName(name) {
			return a.pullCloudModel(ctx, strings.TrimSpace(name), approved)
		}
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
	plan, err := local.PlanFit(a.usableHardware(ctx, existingAncestor(local.HostModelDir(os.Environ()))), localRuntimeConfig(cfg), entry.Requirement())
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

	if err := a.printPullSetup(ctx); err != nil {
		return err
	}
	if ok, err := a.approvePull(entry.Name, approved, "Download and install it now?"); !ok || err != nil {
		return err
	}

	// The pull is the host's own (E10): the bytes land in its store, and
	// `ollama list` shows them afterwards exactly as if the user had typed
	// `ollama pull` — which is what this is, with the fit plan and the
	// approval above in front of it.
	addr, stop, err := a.pullHost(ctx)
	if err != nil {
		return err
	}
	defer stop()
	// Approval and native installation can take minutes and consume the disk
	// the weights will use. Recheck immediately before the model request; the
	// estimate shown before setup is no longer evidence of available capacity.
	if err := ctx.Err(); err != nil {
		return err
	}
	current, err := local.PlanFit(a.usableHardware(ctx, existingAncestor(local.HostModelDir(os.Environ()))), localRuntimeConfig(cfg), entry.Requirement())
	if err != nil {
		return fmt.Errorf("fit changed before the model download: %w; check /localia plan %s", err, entry.Name)
	}
	if current.Placement != plan.Placement || current.GPUIndex != plan.GPUIndex {
		fmt.Fprintf(a.stdout, "  ! Fit now estimates %s", current.Placement)
		if current.Accelerator != "" {
			fmt.Fprintf(a.stdout, " (%s, index %d)", current.Accelerator, current.GPUIndex)
		}
		if current.Fallback != "" {
			fmt.Fprintf(a.stdout, " · %s", current.Fallback)
		}
		fmt.Fprintln(a.stdout)
	}
	fmt.Fprintf(a.stdout, "  └ Fit rechecked · %s disk free · %s available for %s (estimate)\n",
		local.HumanBytes(current.DiskFreeBytes), local.HumanBytes(current.AvailableBytes), current.Placement)
	fmt.Fprintf(a.stdout, "pulling %s through ollama at %s\n", entry.Name, addr)
	if err := local.PullHostModel(ctx, addr, entry.Name, a.stdout); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "%s is pulled; `/model` lists it as ollama/%s\n", entry.Name, entry.Name)
	return nil
}

// pullCloudModel pulls the manifest that lets the session's Ollama route a
// Cloud model to ollama.com. Nothing loads here, so there is no fit to plan,
// but the download is still the user's explicit choice. Going through the
// session's server is what makes this work on a Kolk-managed runtime, whose
// `ollama` is not on PATH and does not listen on the default port.
func (a *app) pullCloudModel(ctx context.Context, name string, approved bool) error {
	if err := validateLocalModelName(name); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "%s (Ollama Cloud)\n", name)
	fmt.Fprintln(a.stdout, "  runs on:   ollama.com; nothing is loaded on this machine")
	fmt.Fprintln(a.stdout, "  download:  its manifest, into this session's Ollama; the weights stay on ollama.com")
	fmt.Fprintln(a.stdout, "  needs:     a server signed in to ollama.com (/plans login ollama <plan>)")
	if err := a.printPullSetup(ctx); err != nil {
		return err
	}
	if ok, err := a.approvePull(name, approved, "Pull its manifest now?"); !ok || err != nil {
		return err
	}
	addr, stop, err := a.pullHost(ctx)
	if err != nil {
		return err
	}
	defer stop()
	// The spelling only claims Cloud. A name ending in -cloud could be any
	// registry model, whose pull would bring weights this text never sized,
	// so the server has to name the remote host before anything is pulled.
	// Only the server's own answer is a verdict about the model; a cancelled,
	// signed-out or failing check says what happened instead.
	remote, err := local.CloudModelRemoteHost(ctx, addr, name)
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.Is(err, local.ErrNotCloudModel):
		return fmt.Errorf("%s is not an Ollama Cloud model on this server; nothing was pulled. `/localia models` lists local models", name)
	case errors.Is(err, local.ErrCloudSignedOut):
		return fmt.Errorf("this Ollama is signed out of ollama.com, so it cannot confirm %s; nothing was pulled. Sign in with /plans login ollama <plan>, then pull again", name)
	case err != nil:
		return fmt.Errorf("could not confirm %s is an Ollama Cloud model: %w; nothing was pulled", name, err)
	}
	fmt.Fprintf(a.stdout, "  └ Cloud model confirmed · runs at %s\n", remote)
	fmt.Fprintf(a.stdout, "pulling %s through ollama at %s\n", name, addr)
	if err := local.PullHostModel(ctx, addr, name, a.stdout); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "%s is pulled; `/model` lists it as ollama/%s\n", name, name)
	return nil
}

// printPullSetup says what else a yes does. A pull goes through a running
// server, so with none the approval also covers starting Ollama and, where
// Kolk can install it, setting it up. The question must cover all of that.
// With no Ollama and no way to set one up here, a yes could only fail, so it
// refuses before the question and names the install instead.
func (a *app) printPullSetup(ctx context.Context) error {
	canSetUp := a.installLocalRuntime != nil && a.managedSetupSupported != nil && a.managedSetupSupported()
	host := a.localHost(ctx)
	companion := ""
	if host.MissingCompanion != "" {
		companion = " with its " + host.MissingCompanion
	}
	if a.waitsOnStalled(host) {
		return fmt.Errorf("%s; a pull works once it answers or that process ends", host.StalledRuntime)
	}
	switch host.State {
	case local.HostInstalled:
		// A managed tree without the accelerator bundle this machine needs
		// is completed first: a new runtime download, beside the old one.
		if host.Managed && companion != "" && canSetUp && host.CompanionFailure != "" {
			// That release is skipped; only a newer one downloads again.
			fmt.Fprintf(a.stdout, "  setup:     a yes also starts Ollama; adding its %s failed before (%s), so only a new Ollama release (a new runtime download) or `/localia setup` retries it\n", host.MissingCompanion, host.CompanionFailure)
			return nil
		}
		if host.Managed && companion != "" && canSetUp {
			fmt.Fprintf(a.stdout, "  setup:     a yes also downloads Ollama again%s, beside the installed runtime, and starts it\n", companion)
			return nil
		}
		fmt.Fprintln(a.stdout, "  setup:     a yes also starts Ollama")
	case local.HostAbsent:
		if !canSetUp {
			return fmt.Errorf("no Ollama to pull into; install it with %s, then run this again", host.InstallHint())
		}
		fmt.Fprintf(a.stdout, "  setup:     no Ollama yet; a yes also sets up Ollama (official build, no Docker or sudo)%s and starts it\n", companion)
	}
	return nil
}

// approvePull asks before a download. Anything but an explicit yes declines;
// --yes is the same consent given in advance.
func (a *app) approvePull(name string, approved bool, question string) (bool, error) {
	if approved {
		return true, nil
	}
	// A session reads the keyboard from its own goroutine, so prompting
	// here would compete with it for the user's keystrokes — the same
	// contention a provider login would cause.
	if a.terminalOwned != nil && a.terminalOwned() {
		// `kolk localia` retired on 2026-09-02, so "another terminal" is no
		// longer a place this can be done: --yes is the answer, and it is
		// the same consent given in advance.
		return false, fmt.Errorf("a pull needs a yes or no, which this session cannot ask for; repeat it as `/localia pull --yes %s`", name)
	}
	if !a.confirmed(question) {
		fmt.Fprintln(a.stdout, "cancelled; nothing was downloaded")
		return false, nil
	}
	return true, nil
}

// pullHost is the server a pull goes through: the running one, else the one
// an approved pull earns by setup and startup. A live session retains that
// runtime for follow-up work under its configured lifetime.
func (a *app) pullHost(ctx context.Context) (string, func(), error) {
	host := a.localHost(ctx)
	switch host.State {
	case local.HostAbsent, local.HostInstalled:
		return a.startHostFor(ctx, host)
	}
	return host.Addr, func() {}, nil
}

// startHostFor brings up the user's idle Ollama for one command, through the
// same starter a session uses, and hands back the way to stop it.
func (a *app) startHostFor(ctx context.Context, host local.Host) (string, func(), error) {
	return a.startHostWith(ctx, host, true)
}

// startHostWith starts the session's runtime; without setup it only starts
// what is installed, as it is, and never downloads a runtime or bundle.
func (a *app) startHostWith(ctx context.Context, host local.Host, setup bool) (string, func(), error) {
	if a.localRuntime != nil {
		ensure := a.localRuntime.Ensure
		if !setup {
			ensure = a.localRuntime.EnsureInstalled
		}
		addr, err := ensure(ctx)
		// The session route owns cleanup, including after a failed pull.
		return addr, func() {}, err
	}
	if a.startHost != nil {
		return a.startHost(ctx, host)
	}
	starter := &local.HostStarter{Binary: host.Binary, Environ: os.Environ(), Out: a.stdout}
	if setup && a.installLocalRuntime != nil {
		dirs, err := a.resolve()
		if err != nil {
			return "", func() {}, err
		}
		starter.Provision = func(ctx context.Context, out io.Writer) (local.Host, error) {
			return a.provisionLocalRuntime(ctx, dirs, out)
		}
	}
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

// printAcceleratorStatus is what /localia and /doctor say about this
// machine's accelerator bundle: missing, failed before, or not served at all.
// waitsOnStalled reports a persistent session whose project runtime runs but
// does not answer. Nothing starts or sets up beside the project's one
// runtime, so no surface may promise a start, a download or a setup.
func (a *app) waitsOnStalled(host local.Host) bool {
	return host.StalledRuntime != "" && a.localRuntime != nil && a.localRuntime.Persistent
}

// printStalledRuntime names a recorded runtime that runs but does not answer,
// and what this session does about it: a persistent session has only that
// one, and an ephemeral one starts its own beside it.
func (a *app) printStalledRuntime(host local.Host) {
	switch {
	case host.StalledRuntime == "":
	case a.waitsOnStalled(host):
		fmt.Fprintf(a.stdout, "  ! %s; local models fail until it answers or that process ends\n", host.StalledRuntime)
	default:
		fmt.Fprintf(a.stdout, "  ! %s; this session starts its own and leaves that one alone\n", host.StalledRuntime)
	}
}

func (a *app) printAcceleratorStatus(host local.Host) {
	switch {
	case host.MissingCompanion != "" && a.waitsOnStalled(host):
		fmt.Fprintf(a.stdout, "  accelerator: %s is not installed; nothing adds it while Kolk's runtime is not answering\n", host.MissingCompanion)
	case host.MissingCompanion != "" && host.CompanionFailure != "" && host.State == local.HostRunning:
		// Its next start skips that release too, until setup forgets it.
		fmt.Fprintf(a.stdout, "  accelerator: %s is not installed; an earlier attempt failed (%s), and the running runtime started without it. After `/localia setup`, its next start retries; a new Ollama release is tried automatically\n", host.MissingCompanion, host.CompanionFailure)
	case host.MissingCompanion != "" && host.CompanionFailure != "":
		fmt.Fprintf(a.stdout, "  accelerator: %s is not installed; an earlier attempt failed (%s). `/localia setup` retries it, and a new Ollama release is tried automatically\n", host.MissingCompanion, host.CompanionFailure)
	case host.MissingCompanion != "" && host.State == local.HostRunning:
		// A running runtime is reused as it is; nothing adds to it.
		fmt.Fprintf(a.stdout, "  accelerator: %s is not installed; the running runtime started without it, and Kolk adds it the next time it starts one\n", host.MissingCompanion)
	case host.MissingCompanion != "":
		fmt.Fprintf(a.stdout, "  accelerator: %s is not installed; the next local setup, pull or model start adds it\n", host.MissingCompanion)
	}
	if host.AcceleratorNote != "" {
		fmt.Fprintf(a.stdout, "  ! %s\n", host.AcceleratorNote)
	}
}
