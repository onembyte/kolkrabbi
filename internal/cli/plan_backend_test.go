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
	wrapped, ok := agent.Backend.(*verifyingBackend)
	if !ok {
		t.Fatalf("backend = %T, want the verifying wrapper", agent.Backend)
	}
	if _, ok := wrapped.inner.(*agentcli.ClaudeBackend); !ok {
		t.Fatalf("wrapped backend = %T, want the Claude provider CLI backend", wrapped.inner)
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
	if _, ok := agent.Backend.(*verifyingBackend); ok {
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

// Switching models inside a session has to switch the provider with them.
// Otherwise the status bar names one model while another provider answers.
func TestSlashModelSwitchesOntoAndOffAPlanBackend(t *testing.T) {
	dirs := isolateConnectorState(t)
	enablePlanConnector(t, dirs)
	a, ag, out := replFixture(t, "")

	if a.slash(context.Background(), ag, "/model claude-opus") {
		t.Fatal("/model must not exit the session")
	}
	if _, ok := ag.Backend.(*verifyingBackend); !ok {
		t.Fatalf("backend = %T after switching to a plan model", ag.Backend)
	}
	if ag.Model != "claude-opus" {
		t.Fatalf("model = %q", ag.Model)
	}
	if !strings.Contains(out.String(), "Claude Max") {
		t.Fatalf("output = %q, want the plan the session now runs on", out.String())
	}

	if a.slash(context.Background(), ag, "/model vendor/ordinary-model") {
		t.Fatal("/model must not exit the session")
	}
	if _, ok := ag.Backend.(*verifyingBackend); ok {
		t.Fatal("switching to an ordinary model left the plan provider answering")
	}
	if ag.Model != "vendor/ordinary-model" {
		t.Fatalf("model = %q", ag.Model)
	}
}

func TestSlashModelRefusesAnUnusablePlanModelWithoutChangingTheSession(t *testing.T) {
	isolateConnectorState(t)
	a, ag, out := replFixture(t, "")
	before, beforeBackend := ag.Model, ag.Backend

	if a.slash(context.Background(), ag, "/model claude-opus") {
		t.Fatal("/model must not exit the session")
	}
	if !strings.Contains(out.String(), `kolk plans login anthropic "Claude Max"`) {
		t.Fatalf("output = %q, want the command that enables it", out.String())
	}
	if ag.Model != before || ag.Backend != beforeBackend {
		t.Fatalf("a refused switch changed the session: model %q backend %T", ag.Model, ag.Backend)
	}
}
