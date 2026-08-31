package cli

import (
	"context"
	"io"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/paths"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/provider/agentcli"
)

// The defect every judge found independently: planModelCatalog has no
// claude-haiku row, so ResolvePlanModel returns ErrNotAPlanModel and
// planBackendFor answers with a nil backend AND a nil error — "ordinary model,
// use the gateway". Routed through that, the one rung this whole feature adds
// would silently fall through to OpenRouter with an id it does not know.
//
// So the port never goes near it, and this test is what says so.
func TestOpeningACheaperRungDoesNotGoThroughThePlanCatalogue(t *testing.T) {
	dirs := isolateHome(t)
	a, _, _ := newTestApp(t, "")
	a.dirs = dirs
	signIn(t, dirs)

	// The catalogue genuinely does not know it — that is the premise.
	manifest, err := provider.LoadConnectors(dirs.ConnectorsFile())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.ResolvePlanModel("claude-haiku", manifest); err == nil {
		t.Fatal("the plan catalogue now knows claude-haiku; this test's premise is stale")
	}

	backend, err := a.subagentBackend()(context.Background(), "claude-haiku", "code", "medium")
	if err != nil {
		t.Fatalf("opening the cheapest rung failed: %v", err)
	}
	if backend == nil {
		t.Fatal("opening claude-haiku produced no backend and no error — the catalogue fall-through")
	}
	if closer, ok := backend.(io.Closer); ok {
		_ = closer.Close()
	}
}

func TestHardCodexSubagentTranslatesCanonicalMaxToXHigh(t *testing.T) {
	dirs := isolateHome(t)
	a, _, _ := newTestApp(t, "")
	a.dirs = dirs
	signInAs(t, dirs, "openai", "ChatGPT Pro", "codex")

	backend, err := a.subagentBackend()(context.Background(), "gpt-5.6-luna", "code", engine.EffortMax)
	if err != nil {
		t.Fatalf("open hard codex subagent: %v", err)
	}
	codex, ok := backend.(*agentcli.CodexBackend)
	if !ok {
		t.Fatalf("backend = %T, want *agentcli.CodexBackend", backend)
	}
	if codex.Effort != "xhigh" {
		t.Fatalf("codex effort = %q, want provider-native xhigh", codex.Effort)
	}
}

// A vendor that is not signed in through kolk offers nothing, which is the same
// rule the roster enforces — stated twice on purpose, because the roster
// decides the menu and this decides the spawn.
func TestASubagentProviderNeedsItsVendorSignedIn(t *testing.T) {
	dirs := isolateHome(t)
	a, _, _ := newTestApp(t, "")
	a.dirs = dirs

	if _, err := a.subagentBackend()(context.Background(), "claude-haiku", "code", "medium"); err == nil {
		t.Error("a rung was opened with no connector signed in")
	}
}

// Subagent spawning must enforce the same provider/name identity as the
// roster. A same-name connector from the wrong provider is not authorization
// for the vendor CLI whose rung is being opened.
func TestASubagentProviderRequiresMatchingProviderIdentity(t *testing.T) {
	dirs := isolateHome(t)
	a, _, _ := newTestApp(t, "")
	a.dirs = dirs
	signInAs(t, dirs, "openai", "ChatGPT Pro", "claude")
	signInAs(t, dirs, "anthropic", "Claude Max", "codex")

	open := a.subagentBackend()
	for _, model := range []string{"claude-haiku", "gpt-5.6-sol"} {
		backend, err := open(context.Background(), model, "code", "medium")
		if closer, ok := backend.(io.Closer); ok {
			_ = closer.Close()
		}
		if err == nil {
			t.Errorf("%s was opened with a same-name connector from the wrong provider", model)
		}
	}
}

// A model that is not a vendor rung is not this port's business: answering
// nothing lets the engine share the session's own provider, which is what a
// gateway session has always done.
func TestAGatewayModelIsLeftToTheSessionProvider(t *testing.T) {
	dirs := isolateHome(t)
	a, _, _ := newTestApp(t, "")
	a.dirs = dirs
	signIn(t, dirs)

	backend, err := a.subagentBackend()(context.Background(), "openrouter/free", "code", "medium")
	if err != nil {
		t.Fatalf("a gateway model produced an error: %v", err)
	}
	if backend != nil {
		t.Error("a gateway model was given its own vendor process")
	}
}

// The load-bearing direction, and the test that fails the day someone edits the
// ladder without editing the aliases: every rung the roster can emit must be a
// model this port can actually open.
func TestEverySpawnableRungIsAModelItsAdapterAccepts(t *testing.T) {
	dirs := isolateHome(t)
	a, _, _ := newTestApp(t, "")
	a.dirs = dirs
	signIn(t, dirs)

	signInAs(t, dirs, "openai", "ChatGPT Pro", "codex")

	open := a.subagentBackend()
	rungs := append(engine.LadderRungIDs("claude"), engine.LadderRungIDs("codex")...)
	for _, id := range rungs {
		backend, err := open(context.Background(), id, "code", "medium")
		if err != nil {
			t.Errorf("rung %q is on the ladder but cannot be opened: %v", id, err)
			continue
		}
		if backend == nil {
			t.Errorf("rung %q opened to nothing, so it would fall through to the gateway", id)
			continue
		}
		if closer, ok := backend.(io.Closer); ok {
			_ = closer.Close()
		}
	}
}

func signIn(t *testing.T, dirs paths.Dirs) {
	t.Helper()
	signInAs(t, dirs, "anthropic", "Claude Max", "claude")
}

func signInAs(t *testing.T, dirs paths.Dirs, providerName, plan, connector string) {
	t.Helper()
	if err := provider.SaveConnector(context.Background(), dirs.ConnectorsFile(), provider.Connector{
		Provider: providerName, Plan: plan, Name: connector,
		LoginOwner: "provider-cli", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
}
