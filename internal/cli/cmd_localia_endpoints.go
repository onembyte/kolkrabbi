package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/local"
	"github.com/onembyte/kolkrabbi/internal/shell"
)

// The local endpoints (plan 37): this machine, a box on the LAN, or one
// exact address such as a Tailscale host — one idea, three reaches.

// addEndpoint probes an address, says what is there, and saves it under a
// name. Nothing is written when the probe finds nothing: an endpoint kolk
// cannot reach is not one worth remembering.
func (a *app) addEndpoint(ctx context.Context, name, addr string) error {
	if err := local.ValidEndpointName(name); err != nil {
		return usagef("%s", err)
	}
	dirs, err := a.resolve()
	if err != nil {
		return err
	}
	cfg, err := config.Load(dirs.ConfigFile())
	if err != nil {
		return err
	}
	if _, taken := cfg.FindEndpoint(name); taken {
		return usagef("%q is already an endpoint; `/localia rm %s` first, or pick another name", name, name)
	}
	runtime, err := a.identifyEndpoint(ctx, addr)
	if err != nil {
		return err
	}
	// Said once, before it is saved, because it is the whole difference
	// between a model on this machine and a model somewhere else.
	if !isThisMachine(addr) {
		fmt.Fprintf(a.stdout, "%s is not this machine: prompts, file contents and tool output will leave this machine for it.\n", addr)
		if strings.HasPrefix(runtime.Base, "http://") {
			fmt.Fprintln(a.stdout, "This is plain HTTP, readable by anything on the network in between; a Tailscale address is carried inside its own tunnel.")
		}
	}
	cfg.PutEndpoint(config.Endpoint{Name: name, Addr: addr, Kind: runtime.Kind, Base: runtime.Base})
	if err := config.Save(dirs.ConfigFile(), cfg); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "%s → %s (%s", name, addr, runtime.Kind)
	if runtime.Version != "" {
		fmt.Fprintf(a.stdout, " %s", runtime.Version)
	}
	fmt.Fprintf(a.stdout, ") at %s\n", runtime.Base)
	printEndpointModels(a, name, runtime.Models)
	return nil
}

// printEndpointModels shows what an endpoint serves, as the ids to use.
func printEndpointModels(a *app, name string, models []string) {
	if len(models) == 0 {
		fmt.Fprintf(a.stdout, "  it lists no models yet; pull one there, then `/localia use %s`\n", name)
		return
	}
	fmt.Fprintln(a.stdout, "  models:")
	for _, model := range models {
		fmt.Fprintf(a.stdout, "    %s/%s\n", name, model)
	}
	fmt.Fprintf(a.stdout, "  use one with: /localia use %s <model>\n", name)
}

// removeEndpoint forgets one.
func (a *app) removeEndpoint(name string) error {
	dirs, err := a.resolve()
	if err != nil {
		return err
	}
	cfg, err := config.Load(dirs.ConfigFile())
	if err != nil {
		return err
	}
	if !cfg.RemoveEndpoint(name) {
		return usagef("there is no endpoint called %q; `/localia list` shows them", name)
	}
	if err := config.Save(dirs.ConfigFile(), cfg); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "forgot %s\n", name)
	return nil
}

// listEndpoints shows every endpoint and where it is.
func (a *app) listEndpoints() error {
	dirs, err := a.resolve()
	if err != nil {
		return err
	}
	cfg, err := config.Load(dirs.ConfigFile())
	if err != nil {
		return err
	}
	if len(cfg.Local.Endpoints) == 0 {
		fmt.Fprintln(a.stdout, "no endpoints yet; add one with `/localia add <name> <host:port>`")
		fmt.Fprintln(a.stdout, "this machine's own Ollama needs no endpoint: its models are already `ollama/<model>`")
		return nil
	}
	fmt.Fprintln(a.stdout, "NAME         ADDRESS                        RUNTIME")
	for _, endpoint := range cfg.Local.Endpoints {
		fmt.Fprintf(a.stdout, "%-12s %-30s %s\n", endpoint.Name, endpoint.Addr, endpoint.Kind)
	}
	return nil
}

// useEndpoint points this session at an endpoint. With no model named it
// lists what the endpoint serves rather than guessing one.
func (a *app) useEndpoint(ctx context.Context, ag *engine.Agent, name, model string) error {
	if name == "here" {
		return a.useThisMachine(ctx, ag, model)
	}
	dirs, err := a.resolve()
	if err != nil {
		return err
	}
	cfg, err := config.Load(dirs.ConfigFile())
	if err != nil {
		return err
	}
	endpoint, known := cfg.FindEndpoint(name)
	if !known {
		return usagef("there is no endpoint called %q; `/localia list` shows them, `/localia add` makes one", name)
	}
	if model == "" {
		runtime, err := a.identifyEndpoint(ctx, endpoint.Addr)
		if err != nil {
			return err
		}
		printEndpointModels(a, name, runtime.Models)
		return nil
	}
	return a.pointSessionAt(ctx, ag, name+"/"+model)
}

// useThisMachine goes back to the Ollama on this machine.
func (a *app) useThisMachine(ctx context.Context, ag *engine.Agent, model string) error {
	if model == "" {
		fmt.Fprintln(a.stdout, "this machine's models are `ollama/<model>`; `/localia` lists what is pulled")
		return nil
	}
	return a.pointSessionAt(ctx, ag, local.SidecarName+"/"+model)
}

// pointSessionAt switches the running session's model, or says what to run
// when there is no session to switch.
func (a *app) pointSessionAt(ctx context.Context, ag *engine.Agent, ref string) error {
	if ag == nil {
		fmt.Fprintf(a.stdout, "start a session and run: /model %s\n", ref)
		return nil
	}
	resolved, err := a.switchModel(ctx, ag, ref)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "model set to %s\n", resolved)
	return nil
}

// identifyEndpoint probes an address through the injected prober.
func (a *app) identifyEndpoint(ctx context.Context, addr string) (local.Runtime, error) {
	if a.identify == nil {
		a.identify = local.Identify
	}
	return a.identify(ctx, addr)
}

// isThisMachine reports whether an address is the loopback, where nothing
// leaves the machine at all.
func isThisMachine(addr string) bool {
	host := addr
	if strings.Contains(addr, "://") {
		if parsed, err := url.Parse(addr); err == nil {
			host = parsed.Host
		}
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// endpointRoutes are the backends every configured endpoint answers with,
// keyed by name — which is the model-id prefix the engine routes on.
func endpointRoutes(cfg *config.Config) map[string]engine.ChatBackend {
	routes := map[string]engine.ChatBackend{}
	for _, endpoint := range cfg.Local.Endpoints {
		if endpoint.Name == "" || endpoint.Base == "" {
			continue
		}
		routes[endpoint.Name] = local.EndpointBackend(endpoint.Kind, endpoint.Base)
	}
	return routes
}

// knownRunners are the local addresses a model runner serves on: Docker's
// model runner first, then Ollama. `direct` asks these, in this order,
// which is enough to find what the user's own command just started.
var knownRunners = []string{"127.0.0.1:12434", "127.0.0.1:11434"}

// directRun runs the user's own model-runner command and then uses what it
// served. The command is theirs: kolk does not write it, does not guess a
// runner and installs nothing. A command that fails switches nothing.
func (a *app) directRun(ctx context.Context, ag *engine.Agent, command []string, approved bool) error {
	if len(command) == 0 {
		return usagef("usage: /localia direct [--yes] <command…>   e.g. /localia direct docker model run hf.co/org/model")
	}
	// The model is the command's last argument, which is where every runner
	// worth the name puts it.
	model := command[len(command)-1]
	line := strings.Join(command, " ")

	if !approved {
		fmt.Fprintf(a.stdout, "this runs on this machine, as you:\n  %s\n", line)
		if a.terminalOwned != nil && a.terminalOwned() {
			return fmt.Errorf("running a command needs a yes or no, which this session cannot ask for; repeat it as `/localia direct --yes %s`", line)
		}
		if !a.confirmed("Run it now?") {
			fmt.Fprintln(a.stdout, "cancelled; nothing was run")
			return nil
		}
	}

	fmt.Fprintf(a.stdout, "· %s\n", line)
	if err := a.runModelRunner(ctx, a.stdout, command); err != nil {
		return fmt.Errorf("%s: %w", line, err)
	}

	// Whatever it started is on this machine; ask the runners kolk knows
	// which of them now serves that model.
	for _, addr := range knownRunners {
		runtime, err := a.identifyEndpoint(ctx, addr)
		if err != nil || !servesModel(runtime.Models, model) {
			continue
		}
		if runtime.Kind == local.KindOllama {
			// This machine's own Ollama already answers for ollama/<model>;
			// a second record for the same server would only be a way to
			// get them out of step.
			fmt.Fprintf(a.stdout, "%s serves it; use %s/%s\n", addr, local.SidecarName, model)
			return a.pointSessionAt(ctx, ag, local.SidecarName+"/"+model)
		}
		name := "direct"
		if err := a.saveEndpoint(config.Endpoint{Name: name, Addr: addr, Kind: runtime.Kind, Base: runtime.Base}); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "%s serves it; use %s/%s\n", addr, name, model)
		return a.pointSessionAt(ctx, ag, name+"/"+model)
	}
	return fmt.Errorf("the command ran, but no runner kolk knows (%s) lists %s; `/localia add <name> <host:port>` points at one directly",
		strings.Join(knownRunners, ", "), model)
}

// servesModel reports whether a runtime lists this model. A runner may
// spell an id differently from the command that asked for it, so a suffix
// counts: "hf.co/org/model" is served by "model" and by itself.
func servesModel(models []string, want string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	for _, model := range models {
		got := strings.ToLower(model)
		if got == want || strings.HasSuffix(want, "/"+got) || strings.HasSuffix(got, "/"+want) {
			return true
		}
	}
	return false
}

// saveEndpoint writes one endpoint record, replacing any of the same name.
func (a *app) saveEndpoint(endpoint config.Endpoint) error {
	dirs, err := a.resolve()
	if err != nil {
		return err
	}
	cfg, err := config.Load(dirs.ConfigFile())
	if err != nil {
		return err
	}
	cfg.PutEndpoint(endpoint)
	return config.Save(dirs.ConfigFile(), cfg)
}

// runModelRunner runs one command, streaming what it prints, through the
// injected seam so the endpoint commands can be tested without a runner.
func (a *app) runModelRunner(ctx context.Context, out io.Writer, command []string) error {
	if a.runRunner == nil {
		a.runRunner = runRunnerCommand
	}
	return a.runRunner(ctx, out, command)
}

// runRunnerCommand is the real thing: the command on this machine, its
// output streamed as it arrives, its stdin closed so an interactive runner
// finishes rather than waiting for a person who is not there.
func runRunnerCommand(ctx context.Context, out io.Writer, command []string) error {
	process, err := shell.StartLinesProcess(ctx, command[0], command[1:])
	if err != nil {
		return err
	}
	defer func() { _ = process.Close() }()
	for {
		line, err := process.Next(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		fmt.Fprintf(out, "  %s\n", line)
	}
}
