package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/provider/agentcli"
)

func enablePlanConnector(t *testing.T, dirs interface{ ConnectorsFile() string }) {
	t.Helper()
	if err := provider.SaveConnector(context.Background(), dirs.ConnectorsFile(), provider.Connector{
		Provider: "anthropic", Plan: "Claude Max", Name: "claude",
		Sandbox: true, LoginOwner: "provider-cli", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
}

// Enabling a connector has to actually change which provider answers a turn.
// Until it does, `kolk plans login` writes metadata nobody reads.
func TestSessionUsesTheClaudeBackendForAnEnabledPlanModel(t *testing.T) {
	dirs := storeFirstRunKey(t)
	enablePlanConnector(t, dirs)
	a, _, _ := newTestApp("")

	agent, err := a.newAgent(context.Background(), &options{model: "claude-opus"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := agent.Backend.(*agentcli.ClaudeBackend); !ok {
		t.Fatalf("backend = %T, want the Claude provider CLI backend", agent.Backend)
	}
}

func TestSessionRefusesAPlanModelWhoseConnectorIsNotEnabled(t *testing.T) {
	storeFirstRunKey(t)
	a, _, _ := newTestApp("")

	_, err := a.newAgent(context.Background(), &options{model: "claude-opus"})
	if err == nil {
		t.Fatal("a plan model without an enabled connector must not start a session")
	}
	if !strings.Contains(err.Error(), `kolk plans login anthropic "Claude Max"`) {
		t.Fatalf("error = %v, want the exact command that enables it", err)
	}
}

func TestSessionKeepsTheDefaultBackendForAnOrdinaryModel(t *testing.T) {
	dirs := storeFirstRunKey(t)
	enablePlanConnector(t, dirs)
	a, _, _ := newTestApp("")

	agent, err := a.newAgent(context.Background(), &options{model: "vendor/ordinary-model"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := agent.Backend.(*agentcli.ClaudeBackend); ok {
		t.Fatal("an ordinary model must keep the default provider client")
	}
}

func TestSessionRefusesAPlanModelWithNoAdapterYet(t *testing.T) {
	dirs := storeFirstRunKey(t)
	if err := provider.SaveConnector(context.Background(), dirs.ConnectorsFile(), provider.Connector{
		Provider: "openai", Plan: "ChatGPT Pro", Name: "codex",
		LoginOwner: "provider-cli", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	a, _, _ := newTestApp("")

	_, err := a.newAgent(context.Background(), &options{model: "o3"})
	if err == nil {
		t.Fatal("a connector with no adapter must say so instead of silently using OpenRouter")
	}
	if !strings.Contains(err.Error(), "codex") {
		t.Fatalf("error = %v, want it to name the connector that is not implemented", err)
	}
}
