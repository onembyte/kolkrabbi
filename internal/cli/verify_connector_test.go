package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
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
	a, _, _ := newTestApp("")
	return a, provider.PlanModel{
		Provider: "anthropic", Plan: "Claude Max", Connector: "claude", Model: "claude-opus",
	}
}

// The only honest proof that a provider CLI is signed in is a turn it actually
// answered. That is a turn the user wanted anyway, so it costs nothing extra.
func TestAnsweredTurnVerifiesTheConnector(t *testing.T) {
	a, planModel := unverifiedClaude(t)
	backend := a.verifyingBackend(stubBackend{}, planModel, "high")

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
	backend := a.verifyingBackend(stubBackend{err: errors.New("provider process exited unsuccessfully")}, planModel, "high")

	if _, _, err := backend.StreamChat(context.Background(), "claude-opus", nil, nil, nil); err == nil {
		t.Fatal("the underlying failure must still reach the caller")
	}

	got := errOut.String()
	if !strings.Contains(got, `kolk plans login anthropic "Claude Max"`) {
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
	backend := a.verifyingBackend(stubBackend{}, planModel, "high")

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
	backend := a.verifyingBackend(stubBackend{err: errors.New("boom")}, planModel, "high")

	for range 3 {
		_, _, _ = backend.StreamChat(context.Background(), "claude-opus", nil, nil, nil)
	}
	if strings.Count(errOut.String(), "kolk plans login") != 1 {
		t.Fatalf("stderr = %q, want the hint exactly once", errOut.String())
	}
}
