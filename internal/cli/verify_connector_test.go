package cli

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/provider/agentcli"
)

type stubBackend struct {
	err error
}

func (b stubBackend) StreamChat(context.Context, string, []provider.Message, []provider.Tool, func(string)) (provider.Message, provider.Meta, error) {
	if b.err != nil {
		return provider.Message{}, provider.Meta{}, b.err
	}
	return provider.Message{Role: "assistant", Content: "hi"}, provider.Meta{}, nil
}

func unverifiedClaude(t *testing.T) (*app, provider.PlanModel) {
	t.Helper()
	dirs := isolateConnectorState(t)
	if err := provider.SaveConnector(context.Background(), dirs.ConnectorsFile(), provider.Connector{
		Provider: "anthropic", Plan: "Claude Max", Name: "claude",
		LoginOwner: "provider-cli", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	a, _, _ := newTestApp(t, "")
	return a, provider.PlanModel{
		Provider: "anthropic", Plan: "Claude Max", Connector: "claude", Model: "claude-opus",
	}
}

// The only honest proof that a provider CLI is signed in is a turn it actually
// answered. That is a turn the user wanted anyway, so it costs nothing extra.
func TestAnsweredTurnVerifiesTheConnector(t *testing.T) {
	a, planModel := unverifiedClaude(t)
	backend := a.verifyingBackend(stubBackend{}, planModel, "code", "high", nil)

	if _, _, err := backend.StreamChat(context.Background(), "claude-opus", nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	dirs, err := a.resolve()
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := provider.LoadConnectors(dirs.ConnectorsFile())
	if err != nil || len(manifest.Connectors) != 1 {
		t.Fatalf("manifest = %+v, err = %v", manifest, err)
	}
	if !manifest.Connectors[0].Verified {
		t.Fatal("a connector that answered a turn is still unverified")
	}
}

func TestAFailedTurnOnAnUnverifiedConnectorExplainsTheLikelyCause(t *testing.T) {
	a, planModel := unverifiedClaude(t)
	var errOut strings.Builder
	a.stderr = &errOut
	backend := a.verifyingBackend(stubBackend{err: errors.New("provider process exited unsuccessfully")}, planModel, "code", "high", nil)

	if _, _, err := backend.StreamChat(context.Background(), "claude-opus", nil, nil, nil); err == nil {
		t.Fatal("the underlying failure must still reach the caller")
	}

	got := errOut.String()
	if !strings.Contains(got, `/plans login anthropic "Claude Max"`) {
		t.Fatalf("stderr = %q, want the command that signs the connector in", got)
	}
	// Demoting on a guessed cause would disable a working connector, so the
	// state must be left exactly as it was.
	dirs, _ := a.resolve()
	manifest, _ := provider.LoadConnectors(dirs.ConnectorsFile())
	if len(manifest.Connectors) != 1 || !manifest.Connectors[0].Enabled || manifest.Connectors[0].Verified {
		t.Fatalf("a failed turn changed connector state: %+v", manifest.Connectors)
	}
}

func TestAVerifiedConnectorIsNotReWrittenOnEveryTurn(t *testing.T) {
	a, planModel := unverifiedClaude(t)
	backend := a.verifyingBackend(stubBackend{}, planModel, "code", "high", nil)

	for range 3 {
		if _, _, err := backend.StreamChat(context.Background(), "claude-opus", nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	dirs, _ := a.resolve()
	manifest, _ := provider.LoadConnectors(dirs.ConnectorsFile())
	if len(manifest.Connectors) != 1 {
		t.Fatalf("connectors = %+v, want exactly one", manifest.Connectors)
	}
}

func TestAFailedTurnExplainsOnlyOnce(t *testing.T) {
	a, planModel := unverifiedClaude(t)
	var errOut strings.Builder
	a.stderr = &errOut
	backend := a.verifyingBackend(stubBackend{err: errors.New("boom")}, planModel, "code", "high", nil)

	for range 3 {
		_, _, _ = backend.StreamChat(context.Background(), "claude-opus", nil, nil, nil)
	}
	if strings.Count(errOut.String(), "/plans login") != 1 {
		t.Fatalf("stderr = %q, want the hint exactly once", errOut.String())
	}
}

// stubHandleBackend wraps a stub and reports the vendor conversation it owns,
// the way agentcli.ClaudeBackend does.
type stubHandleBackend struct {
	stubBackend
	handle string
}

func (b stubHandleBackend) ProviderHandle() string { return b.handle }

// The vendor conversation handle is noted on every successful turn — it exists
// from the moment kolk mints it, before the vendor ever confirms it — so a
// /model switch or a later Kolkrabbi run lands on the same conversation.
func TestAnAnsweredTurnNotesTheVendorHandle(t *testing.T) {
	a, planModel := unverifiedClaude(t)
	noted := ""
	var mu sync.Mutex
	backend := a.verifyingBackend(stubHandleBackend{handle: "vendor-conv-1"}, planModel, "code", "high", func(state string) {
		mu.Lock()
		defer mu.Unlock()
		noted = state
	})

	if _, _, err := backend.StreamChat(context.Background(), "claude-opus", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := backend.StreamChat(context.Background(), "claude-opus", nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if noted != "vendor-conv-1" {
		t.Fatalf("noted provider state = %q, want the vendor handle", noted)
	}
	if handle := backend.ProviderHandle(); handle != "vendor-conv-1" {
		t.Fatalf("ProviderHandle() = %q, want the wrapped backend's handle", handle)
	}
}

// A failed turn must not note anything: a handle learned from a turn that died
// half-way is exactly the kind of state a resume should not trust.
func TestAFailedTurnNotesNothing(t *testing.T) {
	a, planModel := unverifiedClaude(t)
	noted := ""
	backend := a.verifyingBackend(stubHandleBackend{stubBackend: stubBackend{err: errors.New("boom")}, handle: "vendor-conv-1"}, planModel, "code", "high", func(state string) {
		noted = state
	})

	if _, _, err := backend.StreamChat(context.Background(), "claude-opus", nil, nil, nil); err == nil {
		t.Fatal("the underlying failure must reach the caller")
	}
	if noted != "" {
		t.Fatalf("a failed turn noted %q", noted)
	}
}

// A plain backend with no vendor state stays silent through the same path.
func TestABackendWithoutAHandleNotesNothing(t *testing.T) {
	a, planModel := unverifiedClaude(t)
	noted := ""
	backend := a.verifyingBackend(stubBackend{}, planModel, "code", "high", func(state string) {
		noted = state
	})

	if _, _, err := backend.StreamChat(context.Background(), "claude-opus", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if noted != "" {
		t.Fatalf("noted %q from a backend that owns no vendor state", noted)
	}
}

type modelReportingBackend struct {
	model string
	err   error
}

func (b modelReportingBackend) StreamChat(context.Context, string, []provider.Message, []provider.Tool, func(string)) (provider.Message, provider.Meta, error) {
	if b.err != nil {
		return provider.Message{}, provider.Meta{}, b.err
	}
	return provider.Message{Role: "assistant", Content: "hi"}, provider.Meta{Model: b.model}, nil
}

// The first prompt is the verification. A turn that answered promotes the
// model the session asked for to verified and records the exact id the vendor
// resolved it to; a refusal by name retires it; any other failure teaches
// nothing about the catalog.
func TestTheFirstPromptVerifiesTheModelInTheVendorCatalog(t *testing.T) {
	a, planModel := unverifiedClaude(t)
	dirs, err := a.resolve()
	if err != nil {
		t.Fatal(err)
	}
	preview, err := agentcli.ClaudePreviewLister{Gateway: []provider.ModelInfo{{ID: "anthropic/claude-opus-5", ContextLength: 1000000}, {ID: "anthropic/claude-haiku-4.5", ContextLength: 200000}}}.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var store provider.VendorCatalogs
	store.Replace(preview)
	if err := provider.SaveVendorCatalogs(dirs.VendorCatalogFile(), store); err != nil {
		t.Fatal(err)
	}

	backend := a.verifyingBackend(modelReportingBackend{model: "claude-opus-5-20260801"}, planModel, "code", "high", nil)
	if _, _, err := backend.StreamChat(context.Background(), "claude-opus", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	store, err = provider.LoadVendorCatalogs(dirs.VendorCatalogFile())
	if err != nil {
		t.Fatal(err)
	}
	opus, _ := store.Vendors["claude"].Find("claude-opus")
	if opus.Status != provider.StatusVerified || opus.ExactIDs[0] != "claude-opus-5-20260801" {
		t.Fatalf("after the first prompt: %+v, want verified on the vendor's resolved id", opus)
	}
	if haiku, _ := store.Vendors["claude"].Find("claude-haiku"); haiku.Status != provider.StatusUnverified {
		t.Fatalf("a turn on opus touched haiku: %+v", haiku)
	}

	refusing := a.verifyingBackend(modelReportingBackend{err: errors.New("claude exited unsuccessfully: [claude-code:unrecognized_model]")}, planModel, "code", "high", nil)
	_, _, _ = refusing.StreamChat(context.Background(), "claude-haiku", nil, nil, nil)
	store, _ = provider.LoadVendorCatalogs(dirs.VendorCatalogFile())
	if haiku, _ := store.Vendors["claude"].Find("claude-haiku"); haiku.Status != provider.StatusGone {
		t.Fatalf("after a refusal by name: %+v, want gone", haiku)
	}

	failing := a.verifyingBackend(modelReportingBackend{err: errors.New("dial tcp: connection refused")}, planModel, "code", "high", nil)
	_, _, _ = failing.StreamChat(context.Background(), "claude-opus", nil, nil, nil)
	store, _ = provider.LoadVendorCatalogs(dirs.VendorCatalogFile())
	if opus, _ := store.Vendors["claude"].Find("claude-opus"); opus.Status != provider.StatusVerified {
		t.Fatalf("a network failure changed a verified row: %+v", opus)
	}
}
