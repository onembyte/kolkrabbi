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

// The defect every judge found independently, on 2026-08-30: planModelCatalog
// had no claude-haiku row, so ResolvePlanModel returned ErrNotAPlanModel and
// planBackendFor answered with a nil backend AND a nil error — "ordinary
// model, use the gateway". Routed through that, the one rung the feature added
// would silently fall through to OpenRouter with an id it does not know.
//
// The catalogue learned haiku and fable on 2026-09-02 (F3), so that exact
// fall-through can no longer happen for those two — but the port still never
// goes near the catalogue, and this test is what says so: the catalogue
// answers "which plans exist", the factory answers "can this machine spawn
// this model", and for every rung on the ladder the second answer is a
// backend or an error, never nil-and-nil.
func TestOpeningACheaperRungDoesNotGoThroughThePlanCatalogue(t *testing.T) {
	dirs := isolateHome(t)
	a, _, _ := newTestApp(t, "")
	a.dirs = dirs
	signIn(t, dirs)

	for _, rung := range engine.LadderRungIDs("claude") {
		backend, err := a.subagentBackend()(context.Background(), rung, "code", "medium", engine.SubagentCapabilities{Workspace: t.TempDir(), NetworkAccess: true})
		if err != nil {
			t.Fatalf("opening %s failed: %v", rung, err)
		}
		if backend == nil {
			t.Fatalf("opening %s produced no backend and no error — the catalogue fall-through", rung)
		}
		if closer, ok := backend.(io.Closer); ok {
			_ = closer.Close()
		}
	}
}

func TestHardCodexSubagentTranslatesCanonicalMaxToXHigh(t *testing.T) {
	dirs := isolateHome(t)
	a, _, _ := newTestApp(t, "")
	a.dirs = dirs
	signInAs(t, dirs, "openai", "ChatGPT Pro", "codex")

	backend, err := a.subagentBackend()(context.Background(), "gpt-5.6-luna", "code", engine.EffortMax, engine.SubagentCapabilities{Workspace: t.TempDir(), NetworkAccess: true})
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

func TestCapabilityAwareSubagentFactoryRequiresAWorkspaceAndBuildsWithinIt(t *testing.T) {
	dirs := isolateHome(t)
	a, _, _ := newTestApp(t, "")
	a.dirs = dirs
	signInAs(t, dirs, "openai", "ChatGPT Pro", "codex")
	open := a.subagentBackend()

	if _, err := open(context.Background(), "gpt-5.6-luna", "code", "medium", engine.SubagentCapabilities{NetworkAccess: true, Workspace: "relative"}); err == nil {
		t.Fatal("capability-aware factory accepted an unverified relative workspace")
	}
	backend, err := open(context.Background(), "gpt-5.6-luna", "code", "medium", engine.SubagentCapabilities{
		NetworkAccess: true,
		Workspace:     t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := backend.(*agentcli.CodexBackend); !ok {
		t.Fatalf("backend = %T, want *agentcli.CodexBackend", backend)
	}
	if closer, ok := backend.(io.Closer); ok {
		_ = closer.Close()
	}
}

// A vendor that is not signed in through kolk offers nothing, which is the same
// rule the roster enforces — stated twice on purpose, because the roster
// decides the menu and this decides the spawn.
func TestASubagentProviderNeedsItsVendorSignedIn(t *testing.T) {
	dirs := isolateHome(t)
	a, _, _ := newTestApp(t, "")
	a.dirs = dirs

	if _, err := a.subagentBackend()(context.Background(), "claude-haiku", "code", "medium", engine.SubagentCapabilities{Workspace: t.TempDir(), NetworkAccess: true}); err == nil {
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
		backend, err := open(context.Background(), model, "code", "medium", engine.SubagentCapabilities{Workspace: t.TempDir(), NetworkAccess: true})
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

	backend, err := a.subagentBackend()(context.Background(), "openrouter/free", "code", "medium", engine.SubagentCapabilities{Workspace: t.TempDir(), NetworkAccess: true})
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
		backend, err := open(context.Background(), id, "code", "medium", engine.SubagentCapabilities{Workspace: t.TempDir(), NetworkAccess: true})
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
