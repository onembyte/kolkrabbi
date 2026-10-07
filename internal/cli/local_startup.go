package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/local"
	"github.com/onembyte/kolkrabbi/internal/paths"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/session"
)

// Read the same model precedence as startup before consulting remote credentials.
// Selecting or resuming a local model must work without an unrelated API key.
// This peek is read-only; ordinary first-run credential errors remain read-only.
func localStartupSelected(o *options, cfg *config.Config, dirs paths.Dirs) (bool, error) {
	model := o.model
	if model == "" && (o.session != "" || o.resume) {
		var saved *session.Session
		var err error
		if o.session != "" {
			saved, err = session.Load(dirs.Sessions(), o.session)
		} else {
			cwd, _ := os.Getwd()
			saved, err = session.LatestForDir(dirs.Sessions(), cwd)
		}
		if err != nil {
			return false, err
		}
		if saved != nil {
			model = saved.Model
		}
	}
	if model == "" {
		model = cfg.Model
	}
	name, ok := strings.CutPrefix(model, local.HostPrefix)
	if ok {
		return true, validateLocalModelName(name)
	}
	return false, nil
}

func validateLocalModelName(name string) error {
	if name == "" || len(name) > 512 || !utf8.ValidString(name) || strings.HasPrefix(name, ":") || strings.HasSuffix(name, ":") || strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") {
		return fmt.Errorf("local model needs a complete name: /model ollama/<model[:tag]>")
	}
	for _, r := range name {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return fmt.Errorf("local model name contains whitespace or a control character")
		}
	}
	return nil
}

// A local-only session has no implicit remote fallback. Explicitly switching
// to a remote model resolves its configured credentials at selection time.
type localOnlyBackend struct{}

func (localOnlyBackend) StreamChat(context.Context, string, []provider.Message, []provider.Tool, func(string)) (provider.Message, provider.Meta, error) {
	return provider.Message{}, provider.Meta{}, fmt.Errorf("this session has no remote provider configured; choose a local model with /model")
}

func (a *app) printSessionModelCatalog(ctx context.Context, ag *engine.Agent, filter string) error {
	dirs, err := a.locate()
	if err != nil {
		return err
	}
	client := ag.Client
	if active, ok := ag.SessionBackend().(*provider.Client); ok {
		client = active
	}
	var catalogErr error
	if client != nil {
		if err := a.printModelCatalog(ctx, client, dirs.CatalogFile(), false, filter); err != nil {
			catalogErr = err
		}
	}
	a.printHostModels(ctx, dirs.HostCatalogFile(), filter)
	return catalogErr
}
