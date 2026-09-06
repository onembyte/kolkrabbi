package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/onembyte/kolkrabbi/internal/atomicfile"
	"github.com/onembyte/kolkrabbi/internal/buildinfo"
	"github.com/onembyte/kolkrabbi/internal/bus"
	"github.com/onembyte/kolkrabbi/internal/checkpoint"
	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/hooks"
	"github.com/onembyte/kolkrabbi/internal/local"
	"github.com/onembyte/kolkrabbi/internal/mcp"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/provider/agentcli"
	"github.com/onembyte/kolkrabbi/internal/session"
	"github.com/onembyte/kolkrabbi/internal/shell"
	"github.com/onembyte/kolkrabbi/internal/stats"
	"github.com/onembyte/kolkrabbi/protocol"
)

// runDefault is `kolk` with no verb: build an agent, then either run one turn
// (a prompt was given) or hand it to the REPL.
func (a *app) runDefault(ctx context.Context, args []string) (err error) {
	o, err := parseFlags(args)
	if err != nil {
		return err
	}

	ag, err := a.newAgent(ctx, o)
	if err != nil {
		return err
	}
	// Agent mode has a spending lane before the first prompt or turn, just as it
	// does after an in-session mode change. Report it once the session is fully
	// resolved, so plan-backed and gateway-backed startup share the same contract.
	a.reportAgentLane(ag)
	// The engine touches no OS, so the host supplies the look. Measured at
	// 6.7 ms on a 215 MiB repository with 544 files, which is nothing against a
	// turn — but it is per turn, so it was measured rather than assumed.
	ag.DirtyFiles = uncommittedFiles(projectRoot())
	// The user's own hooks, confirmed once per command. Project hooks are
	// G16.3's: a .kolk/hooks.json in a cloned repository is a shell command a
	// stranger wrote, and cloning must not be enough to execute anything.
	if runner := a.newHookRunner(ag.Sess.SessionID(), ag.Effort); runner != nil {
		ag.PostWrite = func(tool, path string) {
			event := hooks.PostWrite
			if tool == "edit_file" {
				event = hooks.PostEdit
			}
			for _, result := range runner.Run(ctx, event, path) {
				if result.Failure != "" {
					fmt.Fprintf(a.stderr, "hook %q: %s\n", result.Command, result.Failure)
				}
			}
		}
	}
	// Written here rather than beside the rest of the header, because these are
	// the values the run *resolved* — a flag left unset reads as empty, and a
	// diagnostic that reports "mode " has recorded the one thing it must not
	// get wrong.
	a.debugLog.Printf("session %s, model %s, mode %s, effort %s, permission %s",
		ag.Sess.SessionID(), ag.SessionModel(), ag.Mode, ag.Effort, ag.Permission)
	defer func() {
		// Every goroutine this run started is joined before it returns; a
		// background refresh is cancelled rather than waited for.
		a.joinBackground()
		// Named last, after everything else has had its say, so the line a
		// person needs to attach to a bug report is the final thing on screen.
		if path := a.debugLog.Path(); path != "" {
			_ = a.debugLog.Close()
			fmt.Fprintf(a.stderr, "debug log: %s\n", path)
		}
		// Released before the backend so a crash in Close still frees the
		// session for the next process.
		if a.sessionHold != nil {
			_ = a.sessionHold.Close()
			a.sessionHold = nil
		}
		if closeErr := ag.Close(); closeErr != nil {
			if err == nil {
				err = closeErr
			} else {
				fmt.Fprintf(a.stderr, "warning: backend close failed: %v\n", closeErr)
			}
		}
	}()

	if a.isStdinPiped != nil && a.isStdinPiped() && a.in != nil {
		piped, err := io.ReadAll(a.in)
		if err == nil && len(bytes.TrimSpace(piped)) > 0 {
			trimmed := string(bytes.TrimSpace(piped))
			if o.prompt != "" {
				o.prompt = o.prompt + "\n\n" + trimmed
			} else {
				o.prompt = trimmed
			}
		}
	}

	if o.prompt != "" {
		// Single-shot: Ctrl+C aborts the run, so the signal context is the
		// whole command's lifetime.
		tctx, stop := signal.NotifyContext(ctx, os.Interrupt)
		defer stop()

		if o.outputFormat == "stream-json" {
			if ag.Bus != nil {
				sub, err := ag.Bus.Subscribe(0)
				if err == nil {
					ag.Out = io.Discard
					done := make(chan struct{})
					go func() {
						defer close(done)
						for env := range sub.Events() {
							frame, err := protocol.EncodeNDJSON(env)
							if err == nil {
								_, _ = a.stdout.Write(frame)
							}
						}
					}()
					turnErr := ag.RunTurn(tctx, o.prompt)
					_ = ag.Bus.Close()
					<-done
					return turnErr
				}
			}
		}

		return ag.RunTurn(tctx, o.prompt)
	}
	if a.canUseTUI() {
		return a.tuiRepl(ctx, ag)
	}
	a.attachInteractiveActivity(ag, true)
	replErr := a.repl(ctx, ag)
	if replErr == nil {
		a.finishSession(ctx, ag)
	}
	return replErr
}

func (a *app) attachInteractiveActivity(ag *engine.Agent, repl bool) {
	if !repl || a.canAnimate == nil || !a.canAnimate() || a.newActivity == nil {
		return
	}
	ag.Activity = a.newActivity(a.stdout)
}

// newAgent resolves config, key, session and model into a ready engine.
func (a *app) newAgent(ctx context.Context, o *options) (*engine.Agent, error) {
	// Before anything else: a mistyped flag should be a mistyped flag, not a
	// message about a missing API key.
	permission := engine.DefaultPermission
	if o.permission != "" {
		resolved, ok := engine.NormalizePermission(o.permission)
		if !ok {
			return nil, usagef("permission %q is not one of ask, auto-approve or full-auto", o.permission)
		}
		permission = resolved
	}

	d, err := a.locate()
	if err != nil {
		return nil, err
	}
	root, err := verifiedProjectRoot()
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(d.ConfigFile())
	if err != nil {
		return nil, err
	}
	if retireLegacyFreeConfig(cfg) {
		fmt.Fprintf(a.stderr,
			"warning: %s is no longer guaranteed free; replacing the old free preset with live free-model discovery\n",
			legacyFreePreset,
		)
		// Persisted, so the migration and its warning happen once. Rewriting the
		// config only in memory made this line the first thing every session said.
		if err := config.Save(d.ConfigFile(), cfg); err != nil {
			fmt.Fprintf(a.stderr, "warning: could not save the migrated config: %v\n", err)
		}
	}

	endpoint := config.ResolveBaseURL(o.baseURL, cfg)
	client, err := providerClientForEndpoint(ctx, endpoint, d.CredentialsFile())
	if err != nil {
		return nil, err
	}
	// Before the first turn, what the vendor's terms say about this key.
	printVendorNotice(a.stderr, client)

	d, err = a.resolve()
	if err != nil {
		return nil, err
	}

	sess, err := a.resolveSession(o)
	if err != nil {
		return nil, err
	}
	// One catalog load for the whole startup, and it never waits on the network
	// while any cache exists: a fresh cache is used, a stale one is used and
	// refreshed behind the prompt, and only a first run with no cache at all
	// fetches, bounded so a slow network cannot hold the prompt hostage. 1.2.2
	// made two catalog requests here, one of them uncached and unbounded, and
	// the blank screen it produced was timed at ten seconds.
	catalog := a.loadCatalog(ctx, client, d.CatalogFile())
	// Every start maps what every signed-in vendor offers, behind the prompt:
	// the model commands show what the vendor said, not what kolk wrote down.
	a.refreshVendorCatalogsInBackground(ctx, catalog)

	// Model precedence: -m flag > the resumed session's model > config > the
	// live zero-cost coding choice. Explicit user choices never cause a catalog
	// request and a resumed session never changes models behind the user's back.
	// Effort follows the same shape: -e flag > the resumed session's dial, so a
	// session continues at the width of work it was left running at.
	effort := o.effort
	if effort == "" {
		effort = sess.Effort
	}
	// One discovery per startup, shared by the connector refresh and the
	// route below. 230 µs, measured.
	var host local.Host
	if a.discoverHost != nil {
		host = a.discoverHost(ctx)
	}
	model := o.model
	if model == "" {
		model = sess.Model
	}
	// Mode follows effort's shape: --mode flag > the resumed session's mode >
	// code. The plan backend needs the same resolved value, because mode is
	// part of its spawn contract.
	mode := o.mode
	if mode == "" {
		mode = sess.SessionMode()
	}
	if mode == "" {
		mode = engine.ModeCode
	}
	// meteredFallback is the gateway model a run can continue on when the
	// subscription it started on runs out. Empty means there is none, and a
	// limit ends the run rather than quietly starting to bill.
	meteredFallback := ""
	if model == "" {
		model = cfg.Model
	}
	if model == "" {
		choice := defaultModelChoice{Model: defaultModel, Free: true}
		if a.chooseDefault != nil {
			choice = a.chooseDefault(catalog)
		}
		// A first run must not start on a billed model because the catalogue
		// happened to list no free coding one (B12.13). The policy governs the
		// substitution, never the preference: free is preferred either way.
		choice = applyFreeExhausted(choice, cfg.Routing.OnFreeExhausted)
		// A subscription that is signed in and has answered before outranks any
		// gateway model: it is already paid for, and billing metered credit
		// while it sits idle is the plainest waste there is (A33.6). Only a
		// connector that is enabled *and* verified counts — "listed" is a row
		// in the matrix, not a capability.
		// The Ollama connector's "verified" is re-read from the server first
		// (E6): a sign-in can lapse, and a claim recorded last month must not
		// pick this session's default. One POST, only when a server runs.
		a.refreshOllamaConnector(ctx, d.ConnectorsFile(), host)
		if connectors, err := provider.LoadConnectors(d.ConnectorsFile()); err == nil {
			gateway := choice.Model
			choice = chooseSessionModel(a.planModels(""), choice, connectors)
			// Remember what the gateway would have answered with. When the
			// subscription runs out mid-run, that is the model there is to fall
			// back to — and it is only a fallback if a subscription was
			// actually chosen over it (A33.7).
			if choice.Model != gateway {
				meteredFallback = gateway
			}
		}
		model = choice.Model
		if model == "" {
			// `stop` returns no model on purpose. Falling back to the router
			// here would substitute exactly what the policy refused, which is
			// how a setting becomes decoration.
			if choice.Refused {
				return nil, fmt.Errorf("%s", choice.Warning)
			}
			model = defaultModel
		}
		if choice.Warning != "" {
			fmt.Fprintf(a.stderr, "warning: %s\n", choice.Warning)
		}
	}
	sess.Model = model

	ckpt, err := checkpoint.Open(sess.CkptDir())
	if err != nil {
		// Checkpointing is a convenience, never a precondition for a turn.
		fmt.Fprintf(a.stderr, "warning: checkpoints disabled: %v\n", err)
		ckpt = nil
	}
	if ckpt != nil {
		// The same root the file tools are confined to, so the snapshot and the
		// path jail cannot disagree about what the project is.
		ckpt.UseShadow(ctx, root)
		if notice := ckpt.Notice(); notice != "" {
			fmt.Fprintf(a.stderr, "%s\n", notice)
		}
	}

	// Marks the session live for as long as this process runs it. Advisory,
	// and deliberately not fatal: a platform without file locks still runs
	// sessions, it just cannot report which are running.
	if held, err := session.Hold(d.Sessions(), sess.ID); err == nil {
		a.sessionHold = held
	}

	if o.debug {
		path := filepath.Join(d.Sessions(), sess.ID+".debug.log")
		if log, err := openDebugLog(path); err == nil {
			a.debugLog = log
			// The header is the half of a bug report people forget to include.
			// Everything here is a fact about this machine and this run; the
			// scrubber runs over it like every other line.
			info := buildinfo.Get()
			a.debugLog.Printf("kolk %s (%s) %s/%s, go %s", info.Version, info.Commit, info.OS, info.Arch, info.Go)
			a.debugLog.Printf("base url %s", client.BaseURL)
		} else {
			fmt.Fprintf(a.stderr, "warning: --debug could not open %s: %v\n", path, err)
		}
	}

	var eventBus *bus.Bus
	if b, err := bus.New(sess.ID, bus.Options{
		SpillPath: filepath.Join(d.Sessions(), sess.ID+".events.ndjson"),
	}); err == nil {
		eventBus = b
	}

	// Kept in memory so a later /model switch and the TUI's model picker
	// resolve against the snapshot startup used, with no further read or request.
	a.catalog = catalog
	freeModels := provider.RankFreeModels(catalog)
	if len(freeModels) == 0 {
		freeModels = provider.RankFreeModels(provider.FallbackCatalogSeed())
	}

	backend, model, err := a.planBackend(model, mode, effort, sess.ProviderStateName(), func(state string) {
		// The vendor conversation handle is noted the moment the backend owns
		// one, and the engine's next save writes it to disk; a failed save only
		// costs the resume, never the turn.
		sess.SetProviderStateName(state)
	}, permission)
	if err != nil {
		return nil, err
	}

	// A misspelled slot is silently ignored otherwise, which means paying for
	// the wrong model for as long as it takes someone to notice — on a setting
	// nobody looks at twice, indefinitely.
	if err := engine.ValidateSlots(cfg.Slots); err != nil {
		fmt.Fprintf(a.stderr, "config: %v\n", err)
	}

	ag := engine.New(engine.Options{
		Client:     client,
		Backend:    backend,
		Model:      model,
		Mode:       mode,
		Effort:     effort,
		Permission: permission,
		Root:       root,
		// Off unless the saved config says on. The session switch is /sandbox.
		Sandbox: sandboxFromConfig(cfg.Sandbox, root, d),
		SubagentCapabilities: engine.SubagentCapabilities{
			Workspace: root,
		},
		// Network for each child is decided per task from this policy, not
		// declared here: a research task may reach the web, an edit task may
		// not, and a vendor with no switch is said to have it rather than
		// pretended not to.
		SubagentNetwork: cfg.SubagentNetwork,
		Sess:            sess,
		Ckpt:            ckpt,
		Cooldowns:       engine.OpenCooldowns(sess.CooldownsFile(), d.CooldownsFile()),
		In:              a.in,
		Out:             a.stdout,
		Recorder:        stats.NewStore(d.Data),
		Tiers:           cfg.Tiers,
		Slots:           cfg.Slots,
		// Each orchestrated task gets its own vendor process rather than
		// sharing the session's: one backend means one conversation and one
		// mutex, so subagents would serialise and write their briefings into a
		// single transcript.
		SubagentBackend: a.subagentBackend(),
		// What the run may climb down to: a cheaper rung of a vendor the user
		// has actually signed into through kolk.
		RungAvailable: a.rungAvailable(),
		// What to do when the plan behind the session runs out: ask (the
		// default), switch to the metered model below, or stop (A33.7).
		OnSubscriptionLimit: cfg.Routing.OnSubscriptionLimit,
		// And what to do when no free model will answer (B12.13).
		OnFreeExhausted: cfg.Routing.OnFreeExhausted,
		// A session paused on a limit comes back on its own unless told to wait
		// for /resume; the monitor confirms the lift without spending tokens,
		// and a handover's check is the sign-in it already has (V35.2b).
		ResumePolicy: cfg.Continuity.Resume,
		// Tool servers, started on first need (plan 16 §3).
		ExtraTools: extraTools(a.newMCPPool(cfg)),
		// The continuity block, aliases folded in (plan 35 §2.4, §2.6).
		ContinuityMode: cfg.EffectiveContinuity().Mode,
		Select:         cfg.EffectiveContinuity().Select,
		Preferred:      cfg.EffectiveContinuity().Preferred,
		Order:          cfg.EffectiveContinuity().Order,
		// What could continue the work when the model stops (plan 35 §2.3).
		Candidates:       func() []continuity.Candidate { return a.continuityCandidates(ctx) },
		HandoverSignedIn: a.connectorSignedIn,
		MeteredModel:     func() string { return meteredFallback },
		// The catalogue the session already fetched, so an unset slot can be
		// resolved by what each role needs instead of collapsing to the effort
		// model (A33.4). Already in memory: this costs nothing to pass.
		Catalog: catalog,
		// This machine's own opinion of each model, read once per session:
		// Aggregate decodes the whole usage log, measured at 226 ms on a 5.9 MB
		// file, so it must never be on a per-plan path. A log that cannot be
		// read costs the opinion, never the session.
		ModelRatings:       modelRatings(d.Data),
		MaxRunCostUSD:      cfg.MaxRunCostUSD,
		MaxConcurrentTasks: cfg.MaxConcurrentTasks,
		Bus:                eventBus,
		PinnedModel:        o.model != "",
		FreeModels:         freeModels,
		ContextWindow:      a.contextWindowFor(model),
		UserMemoryFile:     d.MemoryFile(),
		ArchiveCompaction:  archiveCompaction(d.Sessions(), sess.ID),
	})
	// How the session moves to one of the candidates when asked (plan 35
	// §2.5): the surface owns the backends, so it performs the switch.
	ag.Switch = func(ctx context.Context, c continuity.Candidate) (string, error) {
		return a.switchModel(ctx, ag, c.Ref())
	}
	// The user's own Ollama, when one is running, answers for ollama/<model>
	// through the router (E2, E5). Adopted read-only: this session never
	// stops a server it did not start. Discovery costs 230 µs, measured.
	// No discovery seam means no discovery, never a panic: an app built
	// without one simply has no host route.
	{
		switch host.State {
		case local.HostRunning:
			ag.Routes = map[string]engine.ChatBackend{local.SidecarName: local.NewHostBackend(host.Addr)}
		case local.HostInstalled:
			// Installed and idle: kolk starts one of its own on a port it
			// chooses, lazily — when a host model is first chosen or first
			// asked for a turn (E3b, E8) — measured
			// at 300–440 ms to ready, which is a cost to pay once when asked
			// for and never at every startup — and stops it at exit.
			ag.Routes = map[string]engine.ChatBackend{local.SidecarName: local.NewLazyHostBackend(&local.HostStarter{
				Binary: host.Binary, Environ: os.Environ(), Out: a.stdout,
			})}
		}
	}
	// Rules the user already wrote down apply from the first turn. A stored
	// permission that only takes effect after someone opens /permissions is a
	// permission that was not actually stored.
	a.applyRules(ag)

	// What this run actually runs at is what the session remembers: the dial
	// level, and the connector that answers for it — the same state /effort
	// and /model keep current as the run goes on.
	sess.SetEffort(ag.Effort)
	sess.SetMode(ag.Mode)
	if wrapped, ok := backend.(*verifyingBackend); ok {
		sess.SetConnector(wrapped.plan.Connector)
	} else {
		sess.SetConnector("")
	}
	return ag, nil
}

// firstRunCatalogTimeout bounds the only catalog request startup may wait on:
// the one a machine with no cache at all has to make. Every later start reads
// the cache and refreshes it off the critical path.
const (
	firstRunCatalogTimeout = 4 * time.Second
	// catalogRefreshTimeout bounds the background refresh of a stale cache.
	catalogRefreshTimeout = 30 * time.Second
)

// loadCatalog returns the catalog startup will use and never blocks on the
// network while any cache exists. A stale cache is refreshed in the background;
// a failed first fetch falls back to the built-in seed, silently, because the
// model chooser already reports what it fell back to and a second warning for
// the same fact is noise.
func (a *app) loadCatalog(ctx context.Context, client *provider.Client, path string) []provider.ModelInfo {
	snapCtx, cancel := context.WithTimeout(ctx, firstRunCatalogTimeout)
	defer cancel()
	catalog, stale, err := client.CatalogSnapshot(snapCtx, path, provider.DefaultCatalogTTL)
	if err != nil || len(catalog) == 0 {
		return provider.FallbackCatalogSeed()
	}
	if stale {
		a.startBackground(ctx, func(ctx context.Context) {
			refreshCtx, cancel := context.WithTimeout(ctx, catalogRefreshTimeout)
			defer cancel()
			_ = client.RefreshCatalog(refreshCtx, path)
		})
	}
	return catalog
}

// planBackend selects a provider-owned CLI backend when the session's model
// belongs to a subscription plan the user has already signed into. State is the
// provider-side state carried from the session file (for Claude, the vendor
// conversation handle: empty starts one fresh, non-empty resumes it), and note
// receives new state as the backend learns it. An ordinary
// model keeps the default provider client, and a plan model the user cannot use
// yet stops the session with the reason rather than quietly answering from a
// different provider than the one they asked for.
// planBackend is planBackendFor with the model the session should carry:
// the plan's own model when a plan answered, the reference as typed when
// none did. A plan-qualified reference must never reach a vendor as a model
// name — it did once, and the vendor refused it.
func (a *app) planBackend(model, mode, effort, state string, note func(string), permission engine.Permission) (engine.ChatBackend, string, error) {
	backend, planModel, err := a.planBackendFor(model, mode, effort, state, note, permission)
	if err != nil || backend == nil {
		return backend, model, err
	}
	return backend, planModel.Model, nil
}

// planBackendFor reports the provider that must answer for one model. A nil
// backend with a nil error means "an ordinary model, use the default client".
func (a *app) planBackendFor(model, mode, effort, state string, note func(string), permission engine.Permission) (engine.ChatBackend, provider.PlanModel, error) {
	d, err := a.resolve()
	if err != nil {
		return nil, provider.PlanModel{}, err
	}
	manifest, err := provider.LoadConnectors(d.ConnectorsFile())
	if err != nil {
		return nil, provider.PlanModel{}, err
	}
	planModel, err := a.resolvePlanModel(model, manifest)
	if errors.Is(err, provider.ErrNotAPlanModel) {
		return nil, provider.PlanModel{}, nil
	}
	if err != nil {
		return nil, provider.PlanModel{}, err
	}
	switch planModel.Connector {
	case "claude":
		// Wrapped so the first answered turn confirms the connector the user
		// signed into in another terminal. A stored handle resumes the vendor
		// conversation the session left off in; the vendor replays no argv on
		// resume, so model and effort ride along every time.
		resolved := a.planEffort(effort, planModel)
		bypass := permission == engine.PermissionFullAuto
		inner, err := agentcli.NewClaudeBackendFromHandleWithOptions(planModel.Model, mode, resolved, state, state != "",
			agentcli.ExecutionOptions{BypassPermissions: bypass})
		if err != nil {
			return nil, provider.PlanModel{}, err
		}
		if bypass && mode != engine.ModeChat {
			a.noteClaudeBypassOnce()
		}
		return a.verifyingBackend(inner, planModel, mode, resolved, note), planModel, nil
	case "codex":
		// One-shot per turn, one process per turn, the vendor's own thread
		// resumed through `exec resume`: same handle plumbing, no persistent
		// process to keep. The sandbox is the vendor's own, chosen by kolk's
		// session mode.
		resolved := a.planEffort(effort, planModel)
		// The vendor's own effort set for this model, when discovery has
		// one, so a level the vendor lists today is accepted today.
		inner, err := agentcli.NewCodexBackendFromHandleWithOptions(planModel.Model, mode, resolved, state, state != "",
			agentcli.ExecutionOptions{Efforts: planModel.Efforts})
		if err != nil {
			return nil, provider.PlanModel{}, err
		}
		return a.verifyingBackend(inner, planModel, mode, resolved, note), planModel, nil
	case "copilot":
		// The vendor keeps the conversation (--resume) the way it keeps the
		// login; the rung reaches it only on a named model, since `auto`
		// refuses one; every tool is allowed only under full-auto (V34.4c.2).
		resolved := a.planEffort(effort, planModel)
		inner, err := agentcli.NewCopilotBackendWithOptions(planModel.Model, mode, resolved, state,
			agentcli.ExecutionOptions{BypassPermissions: permission == engine.PermissionFullAuto, Efforts: planModel.Efforts})
		if err != nil {
			return nil, provider.PlanModel{}, err
		}
		return a.verifyingBackend(inner, planModel, mode, resolved, note), planModel, nil
	default:
		return nil, provider.PlanModel{}, fmt.Errorf("the %s connector is enabled but Kolkrabbi has no adapter for it yet, so %s cannot run a session",
			planModel.Connector, planModel.Model)
	}
}

// namedPlanModel reports whether a reference names a subscription plan model,
// and surfaces the reason when it names one the user cannot use.
func (a *app) namedPlanModel(ref string) (bool, error) {
	d, err := a.resolve()
	if err != nil {
		return false, err
	}
	manifest, err := provider.LoadConnectors(d.ConnectorsFile())
	if err != nil {
		return false, err
	}
	if _, err := a.resolvePlanModel(ref, manifest); err != nil {
		if errors.Is(err, provider.ErrNotAPlanModel) {
			return false, nil
		}
		return true, err
	}
	return true, nil
}

// planEffort maps the session's effort onto a level the plan actually offers.
// The dial is a preference, so an unavailable level steps down rather than
// refusing to start a session — but never silently: the substitution is the
// kind of thing a user must be told, or the effort they set means something
// they did not choose.
func (a *app) planEffort(effort string, plan provider.PlanModel) string {
	if canonical, ok := engine.NormalizeEffort(effort); ok {
		effort = canonical
	}
	resolved, changed := provider.EffortForPlan(effort, plan.Efforts)
	if changed {
		fmt.Fprintf(a.stderr, "%s does not offer %s effort; using %s\n", plan.Plan, effort, resolved)
	}
	return resolved
}

// archiveCompaction keeps every conversation a compaction replaced, numbered
// and never overwritten: a second compaction must not erase the record of the
// first, which is the one the user is most likely to want back.
func archiveCompaction(dir, id string) func([]provider.Message) (string, error) {
	return func(messages []provider.Message) (string, error) {
		encoded, err := json.MarshalIndent(messages, "", "  ")
		if err != nil {
			return "", err
		}
		for n := 1; n <= maxCompactionArchives; n++ {
			path := filepath.Join(dir, fmt.Sprintf("%s.pre-compact-%d.json", id, n))
			if _, err := os.Stat(path); err == nil {
				continue
			}
			if err := atomicfile.Write(path, append(encoded, '\n'), 0o600); err != nil {
				return "", err
			}
			return path, nil
		}
		return "", fmt.Errorf("session %s already has %d compaction archives", id, maxCompactionArchives)
	}
}

// maxCompactionArchives bounds what one session can leave on disk.
const maxCompactionArchives = 100

// projectRoot is what file tools are confined to: the repository the user is
// working in, or the directory Kolkrabbi was started in when there is none.
//
// The repository root rather than the working directory, because a coding agent
// asked to fix a test in one package routinely needs a file in another, and a
// jail that fires constantly is one people switch off.
func projectRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	for dir := cwd; ; {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return cwd
		}
		dir = parent
	}
}

// verifiedProjectRoot resolves the host directory once before a provider
// child can be opened. A child receives this canonical absolute path through
// its capability envelope; it never inherits an incidental parent cwd.
func verifiedProjectRoot() (string, error) {
	root := projectRoot()
	if root == "" {
		return "", fmt.Errorf("project workspace is not an absolute directory")
	}
	// One implementation of the four checks, in internal/shell (F6.1).
	return shell.VerifiedDir("project workspace", root)
}

// contextWindowFor reports the advertised context size of one model, or zero
// when the catalog does not describe it. Zero means unknown, and the engine
// treats unknown as "never compact" rather than as a small window.
func (a *app) contextWindowFor(model string) int {
	for _, entry := range a.catalog {
		if entry.ID == model {
			return entry.ContextLength
		}
	}
	return 0
}

// switchModel points a live session at a model and, when that model belongs to
// a subscription plan, at the provider that can actually answer it. Without
// this the status line names one model while a different provider replies.
func (a *app) switchModel(ctx context.Context, ag *engine.Agent, ref string) (string, error) {
	// A model that cannot take tools is refused for a mode that sends them,
	// here, with the sentence plan 06 wrote — rather than as a 400 in the
	// middle of a turn, because the engine sends tool schemas by mode and
	// never by model (E9).
	if err := a.refuseToollessHostModel(ctx, ag, ref); err != nil {
		return "", err
	}
	// The vendor conversation continues across a model switch: the stored
	// handle (from this run or a previous one) resumes the same conversation on
	// the model the user just chose, and new provider state lands back in the
	// session file the same way it does at startup.
	state, note := "", func(string) {}
	if ag.Sess != nil {
		state = ag.Sess.ProviderStateName()
		note = func(state string) { ag.Sess.SetProviderStateName(state) }
	}
	backend, planModel, err := a.planBackendFor(ref, ag.Mode, ag.Effort, state, note, ag.Permission)
	if err != nil {
		return "", err
	}

	previous := ag.SessionBackend()
	resolved, label := provider.ResolveModelAlias(ref), ""
	// Through the setters, not the fields: a turn may be in flight, and in an
	// orchestrated one every subagent is reading the model on its way to a
	// spawn. The engine owns the lock; this package must not write behind it.
	if backend != nil {
		resolved = planModel.Model
		label = fmt.Sprintf(" (%s, via the %s CLI)", planModel.Plan, planModel.Connector)
		ag.SetSessionBackend(backend)
	} else if ag.Client != nil {
		ag.SetSessionBackend(ag.Client)
	}
	// The retired provider owns a child process; nothing else will release it.
	if current := ag.SessionBackend(); previous != nil && previous != current {
		if closer, ok := previous.(io.Closer); ok {
			_ = closer.Close()
		}
	}

	// Options is embedded in Agent, so these are the session's own fields and a
	// later /new inherits the provider it is running on.
	ag.SetSessionModel(resolved)
	// A provider CLI's window is not in the catalog, so a plan model reports
	// unknown rather than borrowing the previous model's limit. A host model's
	// comes from its route, which the engine asks when this is zero (E8).
	ag.ContextWindow = a.contextWindowFor(resolved)
	a.warmHostModel(ctx, ag, resolved)
	ag.PinnedModel = true
	if ag.Sess != nil {
		ag.Sess.SetModelName(resolved)
		// The connector the session runs on now is session state too, so the
		// card and a later resume say the same thing the run does.
		if wrapped, ok := backend.(*verifyingBackend); ok {
			ag.Sess.SetConnector(wrapped.plan.Connector)
		} else {
			ag.Sess.SetConnector("")
		}
	}
	return resolved + label, nil
}

// resolveSession picks the session this run continues: an explicit id, the most
// recent one, or a fresh one.
func (a *app) resolveSession(o *options) (*session.Session, error) {
	d, err := a.resolve()
	if err != nil {
		return nil, err
	}
	if err := d.EnsureData(); err != nil {
		return nil, err
	}
	sdir := d.Sessions()
	var sess *session.Session
	switch {
	case o.session != "":
		sess, err = session.Load(sdir, o.session)
		if err != nil {
			return nil, fmt.Errorf("cannot load session %s: %w (try `kolk sessions`)", o.session, err)
		}
	case o.resume:
		cwd, _ := os.Getwd()
		sess, err = session.LatestForDir(sdir, cwd)
		if err != nil {
			return nil, err
		}
		if sess == nil {
			fmt.Fprintln(a.stdout, "no previous session found, starting a new one.")
		} else if sess.CWD != "" && cwd != "" && filepath.Clean(sess.CWD) != filepath.Clean(cwd) {
			// Resuming something started elsewhere is legitimate but surprising,
			// so it is stated rather than discovered from the transcript.
			fmt.Fprintf(a.stdout, "resuming a session started in %s\n", sess.CWD)
		}
	}
	if sess == nil {
		sess = session.New(sdir, "")
	}
	return sess, nil
}

// modelRatings folds this machine's ratings for the engine's slot selection.
//
// The conversion exists because the engine may not import internal/stats — it
// sits a layer below the adapters — so the host reads the log and hands over
// plain values, the same shape as DirtyFiles and the hook runner.
func modelRatings(dataDir string) map[string]engine.ModelRating {
	folded, err := stats.RatingsByModel(dataDir)
	if err != nil || len(folded) == 0 {
		return nil
	}
	ratings := make(map[string]engine.ModelRating, len(folded))
	for model, rating := range folded {
		ratings[model] = engine.ModelRating{Average: rating.Average, Count: rating.Count}
	}
	return ratings
}

// refreshOllamaConnector brings a recorded Ollama connector in line with what
// the running server says about its sign-in — in both directions, saved only
// on a change. A connector that is not recorded is left alone: kolk never
// invents one from a sign-in nobody asked it to use.
func (a *app) refreshOllamaConnector(ctx context.Context, connectorsFile string, host local.Host) {
	if host.State != local.HostRunning || a.signIn == nil {
		return
	}
	manifest, err := provider.LoadConnectors(connectorsFile)
	if err != nil {
		return
	}
	for _, connector := range manifest.Connectors {
		if connector.Name != local.SidecarName || !connector.Enabled {
			continue
		}
		state := a.signIn(ctx, host.Addr)
		if !state.Known || state.SignedIn == connector.Verified {
			return
		}
		connector.Verified = state.SignedIn
		_ = provider.SaveConnector(ctx, connectorsFile, connector)
		return
	}
}

// modelWarmer is what a host route implements to load a model ahead of its
// first turn.
type modelWarmer interface {
	Warm(context.Context, string)
}

// warmHostModel loads a just-selected host model in the background, so the
// first turn is not a cold load — seconds for a 7B, minutes on a CPU — and the
// server's effective window is known before the first prompt is built. Off
// the turn path and bounded: a warm that fails costs nothing but the warmth.
func (a *app) warmHostModel(ctx context.Context, ag *engine.Agent, model string) {
	prefix, wire, ok := strings.Cut(model, "/")
	if !ok {
		return
	}
	warmer, ok := ag.Routes[prefix].(modelWarmer)
	if !ok {
		return
	}
	warm := a.warmHost
	if warm == nil {
		warm = func(ctx context.Context, w modelWarmer, model string) {
			// Off the turn path, but not off the session: a warm that
			// outlived the session would load a model for nobody.
			go func() {
				ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
				defer cancel()
				w.Warm(ctx, model)
			}()
		}
	}
	warm(ctx, warmer, wire)
}

// refuseToollessHostModel is the selection-time check for a host model the
// server says has no tool capability. Chat mode sends no tools and is fine;
// code and agent mode would 400 on the first turn.
func (a *app) refuseToollessHostModel(ctx context.Context, ag *engine.Agent, ref string) error {
	name, ok := strings.CutPrefix(ref, local.HostPrefix)
	if !ok || ag.Mode == engine.ModeChat || a.discoverHost == nil || a.listHostModels == nil {
		return nil
	}
	host := a.discoverHost(ctx)
	if host.State != local.HostRunning {
		return nil
	}
	cache := ""
	if d, err := a.locate(); err == nil {
		cache = d.HostCatalogFile()
	}
	// A listing that fails must not block a selection: the worst case is the
	// 400 this check exists to pre-empt, which is where things stood before.
	models, _ := a.listHostModels(ctx, host.Addr, cache)
	for _, m := range models {
		if m.Name == name && m.CapabilitiesKnown && !m.Tools {
			return fmt.Errorf("%s is unavailable in %s mode: no tool support. `/mode chat` can use it; code and agent mode need a tool-capable model (`/model` marks them)", ref, ag.Mode)
		}
	}
	return nil
}

// extraTools keeps a nil pool from becoming a non-nil interface.
func extraTools(pool *mcp.Pool) engine.ExtraTools {
	if pool == nil {
		return nil
	}
	return pool
}
