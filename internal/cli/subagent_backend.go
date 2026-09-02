package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/provider/agentcli"
)

// subagentBackend opens one task's own vendor provider.
//
// It deliberately does NOT go through planBackendFor, and that is the whole
// point of this function existing separately.
//
// planBackendFor resolves through provider.ResolvePlanModel, which answers
// ErrNotAPlanModel for anything the catalogue does not list, and planBackendFor
// turns that into a nil backend and a **nil error**, meaning "ordinary model,
// use the gateway". When the catalogue had no `claude-haiku` row (until
// 2026-09-02) a subagent routed that way would quietly have asked OpenRouter
// for a model id it has never heard of, and the feature would have looked
// built while changing nothing. The catalogue lists every Claude rung now; the
// reason stands regardless, because the next rung someone adds to a ladder
// will not be in the catalogue on the day it is added.
//
// So the adapter is constructed straight from the connector manifest. The
// catalogue answers "which plans exist", which is a different question from
// "can this machine spawn this model", and only the second one matters here.
func (a *app) subagentBackend() engine.SubagentBackend {
	return func(ctx context.Context, model, mode, effort string, capabilities engine.SubagentCapabilities) (engine.ChatBackend, error) {
		store := a.vendorCatalogs()
		vendor := ""
		switch {
		case a.vendorKnowsModel(store, "claude", model):
			vendor = "claude"
		case a.vendorKnowsModel(store, "codex", model):
			vendor = "codex"
		default:
			// Not a vendor rung: not this port's business. Answering nothing
			// lets the engine share the session's own provider, which is what
			// a gateway session has always done and what a nil port means.
			return nil, nil
		}
		if !a.connectorSignedIn(vendor) {
			return nil, fmt.Errorf("cannot run %s: the %s connector is not signed in (/plans login <provider> <plan>)", model, vendor)
		}
		execution := agentcli.ExecutionOptions{
			Workspace:      capabilities.Workspace,
			AdditionalDirs: capabilities.AdditionalDirs,
			NetworkAccess:  capabilities.NetworkAccess,
			Provider:       vendor,
			Efforts:        a.discoveredEfforts(store, vendor, model),
		}
		if vendor == "codex" {
			// One thread per subagent. Sharing one was the whole reason codex
			// refused agent mode: every turn resumes the backend's own thread,
			// so several subagents on one backend would interleave into a
			// single vendor transcript.
			return agentcli.NewCodexBackendFromHandleWithOptions(model, mode, effort, "", false, execution)
		}
		// Empty handle, resume false: a conversation of its own, minted fresh
		// and never persisted. A subagent's handle must not become the
		// session's resume handle — the session is a different conversation,
		// and inheriting a child's would resume the wrong one.
		//
		// Unwrapped by verifyingBackend on purpose: that wrapper exists to
		// confirm the connector on its first answered turn and to record it,
		// and a subagent is not where a session-level fact should be decided.
		return agentcli.NewClaudeBackendFromHandleWithOptions(model, mode, effort, "", false, execution)
	}
}

// connectorProvider returns the provider identity for an adapter connector.
// The engine's ladders use adapter names (claude, codex), while connector
// manifests identify the provider separately (anthropic, openai). Keeping that
// mapping here prevents availability callers from checking only one half of
// the connector identity.
func connectorProvider(name string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "claude":
		return "anthropic", true
	case "codex":
		return "openai", true
	default:
		return "", false
	}
}

// connectorSignedIn reports whether the exact provider/name connector has been
// signed into through kolk. Enabled is sufficient by design; verification is a
// stronger runtime claim and is not required to make a freshly signed-in rung
// visible.
func (a *app) connectorSignedIn(name string) bool {
	connectorName := strings.ToLower(strings.TrimSpace(name))
	expectedProvider, ok := connectorProvider(connectorName)
	if !ok {
		return false
	}
	dirs, err := a.locate()
	if err != nil {
		return false
	}
	manifest, err := provider.LoadConnectors(dirs.ConnectorsFile())
	if err != nil {
		return false
	}
	for _, connector := range manifest.Connectors {
		if connector.Provider == expectedProvider &&
			connector.Name == connectorName && connector.Enabled {
			return true
		}
	}
	return false
}
