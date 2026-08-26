package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/onembyte/kolkrabbi/internal/bus"
	"github.com/onembyte/kolkrabbi/internal/checkpoint"
	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/provider"
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

	var freeModels []string
	if models, err := client.ListModelsCached(ctx, d.CatalogFile(), provider.DefaultCatalogTTL, false); err == nil {
		freeModels = provider.RankFreeModels(models)
	}
	if len(freeModels) == 0 {
		freeModels = provider.RankFreeModels(provider.FallbackCatalogSeed())
	}

	return engine.New(engine.Options{
		Client:      client,
		Model:       model,
		Mode:        o.mode,
		Effort:      o.effort,
		Yolo:        o.yolo,
		Sess:        sess,
		Ckpt:        ckpt,
		In:          a.in,
		Out:         a.stdout,
		Recorder:    stats.NewStore(d.Data),
		Tiers:       cfg.Tiers,
		Bus:         eventBus,
		PinnedModel: o.model != "",
		FreeModels:  freeModels,
	}), nil
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
		sess, err = session.Latest(sdir)
		if err != nil {
			return nil, err
		}
		if sess == nil {
			fmt.Fprintln(a.stdout, "no previous session found, starting a new one.")
		}
	}
	if sess == nil {
		sess = session.New(sdir, "")
	}
	return sess, nil
}
