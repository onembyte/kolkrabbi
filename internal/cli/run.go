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

	"github.com/onembyte/kolkrabbi/internal/atomicfile"
	"github.com/onembyte/kolkrabbi/internal/bus"
	"github.com/onembyte/kolkrabbi/internal/checkpoint"
	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/provider/agentcli"
	"github.com/onembyte/kolkrabbi/internal/session"
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
	defer func() {
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
	return a.repl(ctx, ag)
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
	cfg, err := config.Load(d.ConfigFile())
	if err != nil {
		return nil, err
	}
	if retireLegacyFreeConfig(cfg) {
		fmt.Fprintf(a.stderr,
			"warning: %s is no longer guaranteed free; replacing the old free preset with live free-model discovery\n",
			legacyFreePreset,
		)
	}

	apiKey, err := resolveOpenRouterCredential(ctx, d.CredentialsFile())
	if err != nil {
		return nil, err
	}
	if apiKey.IsZero() {
		return nil, guidedAction("kolk needs an API key before it can use models.\n" +
			"Add one:  kolk key <API_KEY>\n" +
			"Then run: kolk")
	}

	d, err = a.resolve()
	if err != nil {
		return nil, err
	}

	sess, err := a.resolveSession(o)
	if err != nil {
		return nil, err
	}
	client := provider.NewClient(apiKey.Reveal())
	client.BaseURL = config.ResolveBaseURL(o.baseURL, cfg)

	// Model precedence: -m flag > the resumed session's model > config > the
	// live zero-cost coding choice. Explicit user choices never cause a catalog
	// request and a resumed session never changes models behind the user's back.
	model := o.model
	if model == "" {
		model = sess.Model
	}
	if model == "" {
		model = cfg.Model
	}
	if model == "" {
		choice := defaultModelChoice{Model: defaultModel, Free: true}
		if a.chooseDefault != nil {
			choice = a.chooseDefault(ctx, client)
		}
		model = choice.Model
		if model == "" {
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

	var eventBus *bus.Bus
	if b, err := bus.New(sess.ID, bus.Options{
		SpillPath: filepath.Join(d.Sessions(), sess.ID+".events.ndjson"),
	}); err == nil {
		eventBus = b
	}

	catalog, err := client.ListModelsCached(ctx, d.CatalogFile(), provider.DefaultCatalogTTL, false)
	if err != nil {
		catalog = provider.FallbackCatalogSeed()
	}
	// Kept in memory so a later /model switch can resolve the new model's
	// window without a network call.
	a.catalog = catalog
	freeModels := provider.RankFreeModels(catalog)
	if len(freeModels) == 0 {
		freeModels = provider.RankFreeModels(provider.FallbackCatalogSeed())
	}

	backend, err := a.planBackend(model, o.effort)
	if err != nil {
		return nil, err
	}

	ag := engine.New(engine.Options{
		Client:            client,
		Backend:           backend,
		Model:             model,
		Mode:              o.mode,
		Effort:            o.effort,
		Permission:        permission,
		Root:              projectRoot(),
		Sess:              sess,
		Ckpt:              ckpt,
		In:                a.in,
		Out:               a.stdout,
		Recorder:          stats.NewStore(d.Data),
		Tiers:             cfg.Tiers,
		Bus:               eventBus,
		PinnedModel:       o.model != "",
		FreeModels:        freeModels,
		ContextWindow:     a.contextWindowFor(model),
		UserMemoryFile:    d.MemoryFile(),
		ArchiveCompaction: archiveCompaction(d.Sessions(), sess.ID),
	})
	// Rules the user already wrote down apply from the first turn. A stored
	// permission that only takes effect after someone opens /permissions is a
	// permission that was not actually stored.
	a.applyRules(ag)
	return ag, nil
}

// planBackend selects a provider-owned CLI backend when the session's model
// belongs to a subscription plan the user has already signed into. An ordinary
// model keeps the default provider client, and a plan model the user cannot use
// yet stops the session with the reason rather than quietly answering from a
// different provider than the one they asked for.
func (a *app) planBackend(model, effort string) (engine.ChatBackend, error) {
	backend, _, err := a.planBackendFor(model, effort)
	return backend, err
}

// planBackendFor reports the provider that must answer for one model. A nil
// backend with a nil error means "an ordinary model, use the default client".
func (a *app) planBackendFor(model, effort string) (engine.ChatBackend, provider.PlanModel, error) {
	d, err := a.resolve()
	if err != nil {
		return nil, provider.PlanModel{}, err
	}
	manifest, err := provider.LoadConnectors(d.ConnectorsFile())
	if err != nil {
		return nil, provider.PlanModel{}, err
	}
	planModel, err := provider.ResolvePlanModel(model, manifest)
	if errors.Is(err, provider.ErrNotAPlanModel) {
		return nil, provider.PlanModel{}, nil
	}
	if err != nil {
		return nil, provider.PlanModel{}, err
	}
	switch planModel.Connector {
	case "claude":
		// Wrapped so the first answered turn confirms the connector the user
		// signed into in another terminal.
		resolved := a.planEffort(effort, planModel)
		return a.verifyingBackend(agentcli.NewClaudeBackend(resolved), planModel, resolved), planModel, nil
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
	if _, err := provider.ResolvePlanModel(ref, manifest); err != nil {
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
func (a *app) switchModel(ag *engine.Agent, ref string) (string, error) {
	backend, planModel, err := a.planBackendFor(ref, ag.Effort)
	if err != nil {
		return "", err
	}

	previous := ag.Backend
	resolved, label := provider.ResolveModelAlias(ref), ""
	if backend != nil {
		resolved = planModel.Model
		label = fmt.Sprintf(" (%s, via the %s CLI)", planModel.Plan, planModel.Connector)
		ag.Backend = backend
	} else if ag.Client != nil {
		ag.Backend = ag.Client
	}
	// The retired provider owns a child process; nothing else will release it.
	if previous != nil && previous != ag.Backend {
		if closer, ok := previous.(io.Closer); ok {
			_ = closer.Close()
		}
	}

	// Options is embedded in Agent, so these are the session's own fields and a
	// later /new inherits the provider it is running on.
	ag.Model = resolved
	// A provider CLI's window is not in the catalog, so a plan model reports
	// unknown rather than borrowing the previous model's limit.
	ag.ContextWindow = a.contextWindowFor(resolved)
	ag.PinnedModel = true
	if ag.Sess != nil {
		ag.Sess.SetModelName(resolved)
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
