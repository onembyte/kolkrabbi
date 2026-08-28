package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/provider/agentcli"
	"github.com/onembyte/kolkrabbi/internal/session"
)

// A session that ran at an effort (and on a plan) remembers both: the resume
// lands where the work left off, not at the configured default.
func TestResumeRestoresEffortAndConnectorFromTheSession(t *testing.T) {
	dirs := storeFirstRunKey(t)
	if err := dirs.EnsureData(); err != nil {
		t.Fatal(err)
	}
	enablePlanConnector(t, dirs)
	resumed := session.New(dirs.Sessions(), "claude-opus")
	resumed.SetEffort("high")
	resumed.SetConnector("claude")
	if err := resumed.Save(); err != nil {
		t.Fatal(err)
	}

	a, _, _ := newTestApp(t, "")
	ag, err := a.newAgent(context.Background(), &options{session: resumed.ID})
	if err != nil {
		t.Fatal(err)
	}
	if ag.Effort != "high" {
		t.Fatalf("dial effort = %q, want the session's high", ag.Effort)
	}
	if got := ag.Sess.ConnectorName(); got != "claude" {
		t.Fatalf("connector = %q, want claude", got)
	}
	if wrapped, ok := ag.Backend.(*verifyingBackend); ok {
		if inner, ok := wrapped.inner.(*agentcli.ClaudeBackend); !ok || inner.Effort != "high" || inner.Model != "claude-opus" {
			t.Fatalf("inner = %#v, want the claude backend at the restored effort and model", wrapped.inner)
		}
	} else {
		t.Fatalf("backend = %T, want the verifying wrapper", ag.Backend)
	}
}

// A session dial the -e flag overrides stays overridden: the flag is the
// session's model choice's equal, not its inferior.
func TestEffortFlagBeatsTheStoredSessionEffort(t *testing.T) {
	dirs := storeFirstRunKey(t)
	if err := dirs.EnsureData(); err != nil {
		t.Fatal(err)
	}
	stored := session.New(dirs.Sessions(), "vendor/model")
	stored.SetEffort("low")
	if err := stored.Save(); err != nil {
		t.Fatal(err)
	}

	a, _, _ := newTestApp(t, "")
	ag, err := a.newAgent(context.Background(), &options{session: stored.ID, effort: "max"})
	if err != nil {
		t.Fatal(err)
	}
	if ag.Effort != engine.EffortMax {
		t.Fatalf("dial effort = %q, want the -e flag's max", ag.Effort)
	}
}

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
	a, _, _ := newTestApp(t, "")

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
	a, _, _ := newTestApp(t, "")

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
	a, _, _ := newTestApp(t, "")

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
	a, _, _ := newTestApp(t, "")

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

// Effort levels are per plan: Claude Pro stops at high. Passing max straight
// through would let the provider decide what the user meant.
func TestSessionStepsEffortDownToWhatThePlanOffers(t *testing.T) {
	dirs := storeFirstRunKey(t)
	enablePlanConnector(t, dirs)
	a, _, errOut := newTestApp(t, "")

	agent, err := a.newAgent(context.Background(), &options{model: "claude-sonnet", effort: "max"})
	if err != nil {
		t.Fatal(err)
	}

	wrapped, ok := agent.Backend.(*verifyingBackend)
	if !ok {
		t.Fatalf("backend = %T", agent.Backend)
	}
	claude, ok := wrapped.inner.(*agentcli.ClaudeBackend)
	if !ok {
		t.Fatalf("wrapped backend = %T", wrapped.inner)
	}
	if claude.Effort != "high" {
		t.Fatalf("provider effort = %q, want the highest level Claude Pro offers", claude.Effort)
	}
	if !strings.Contains(errOut.String(), "Claude Pro") || !strings.Contains(errOut.String(), "high") {
		t.Fatalf("stderr = %q, want the substitution named", errOut.String())
	}
}

func TestSessionKeepsAnEffortThePlanOffers(t *testing.T) {
	dirs := storeFirstRunKey(t)
	enablePlanConnector(t, dirs)
	a, _, errOut := newTestApp(t, "")

	agent, err := a.newAgent(context.Background(), &options{model: "claude-opus", effort: "max"})
	if err != nil {
		t.Fatal(err)
	}
	wrapped := agent.Backend.(*verifyingBackend)
	if claude := wrapped.inner.(*agentcli.ClaudeBackend); claude.Effort != "max" {
		t.Fatalf("provider effort = %q, want max on a plan that offers it", claude.Effort)
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr = %q, want silence when nothing changed", errOut.String())
	}
}

func TestSessionNormalisesALegacyEffortBeforeCheckingThePlan(t *testing.T) {
	dirs := storeFirstRunKey(t)
	enablePlanConnector(t, dirs)
	a, _, errOut := newTestApp(t, "")

	// "ultra" is the legacy spelling of max; Claude Pro still stops at high.
	agent, err := a.newAgent(context.Background(), &options{model: "claude-sonnet", effort: "ultra"})
	if err != nil {
		t.Fatal(err)
	}
	wrapped := agent.Backend.(*verifyingBackend)
	if claude := wrapped.inner.(*agentcli.ClaudeBackend); claude.Effort != "high" {
		t.Fatalf("provider effort = %q, want a legacy alias resolved before the plan check", claude.Effort)
	}
	if !strings.Contains(errOut.String(), "high") {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

// A provider process is started with its effort and replays no argv, so a new
// level means a new process. The dial does the restart itself: telling the
// user to find a second command again would leave /effort describing a level
// the provider never runs at.
func TestSlashEffortRestartsThePlanProvider(t *testing.T) {
	dirs := isolateConnectorState(t)
	enablePlanConnector(t, dirs)
	a, ag, out := replFixture(t, "")
	if a.slash(context.Background(), ag, "/model claude-opus") {
		t.Fatal("/model must not exit the session")
	}
	before, ok := ag.Backend.(*verifyingBackend)
	if !ok {
		t.Fatalf("backend = %T, want the verifying wrapper before the change", ag.Backend)
	}

	if a.slash(context.Background(), ag, "/effort low") {
		t.Fatal("/effort must not exit the session")
	}
	wrapped, ok := ag.Backend.(*verifyingBackend)
	if !ok {
		t.Fatalf("backend = %T, want the verifying wrapper after /effort", ag.Backend)
	}
	if wrapped.effort != "low" {
		t.Fatalf("provider effort = %q, want the level the dial asked for", wrapped.effort)
	}
	if ag.Backend == before {
		t.Fatal("/effort must replace a plan provider still running at the old level")
	}
	if inner, ok := wrapped.inner.(*agentcli.ClaudeBackend); !ok || inner.Effort != "low" || inner.Model != "claude-opus" {
		t.Fatalf("inner = %#v, want a claude backend spawned at low effort for claude-opus", wrapped.inner)
	}
	got := out.String()
	if strings.Contains(got, "re-run /model") {
		t.Fatalf("output = %q, the manual follow-up command is gone", got)
	}
	if !strings.Contains(got, "restarted at low effort") {
		t.Fatalf("output = %q, want it to say the provider restarted", got)
	}
}

// A /effort that is a no-op on the plan's ladder must not churn the provider
// process: restarting the vendor child costs its streamed context.
func TestSlashEffortLeavesThePlanProviderAloneAtTheSameLevel(t *testing.T) {
	dirs := isolateConnectorState(t)
	enablePlanConnector(t, dirs)
	a, ag, _ := replFixture(t, "")
	if a.slash(context.Background(), ag, "/model claude-opus") {
		t.Fatal("/model must not exit the session")
	}
	_, ok := ag.Backend.(*verifyingBackend)
	if !ok {
		t.Fatalf("backend = %T, want the verifying wrapper", ag.Backend)
	}
	providerEffort := ag.Backend.(*verifyingBackend).effort
	before := ag.Backend

	if a.slash(context.Background(), ag, "/effort "+providerEffort) {
		t.Fatal("/effort must not exit the session")
	}
	if ag.Backend != before {
		t.Fatal("the same effort level must not restart the provider process")
	}
}

func TestSwitchingModelsUpdatesTheContextWindow(t *testing.T) {
	isolateConnectorState(t)
	a, ag, _ := replFixture(t, "")
	a.catalog = []provider.ModelInfo{
		{ID: "vendor/small", ContextLength: 8_000},
		{ID: "vendor/large", ContextLength: 200_000},
	}

	if a.slash(context.Background(), ag, "/model vendor/large") {
		t.Fatal("/model must not exit the session")
	}
	if ag.ContextWindow != 200_000 {
		t.Fatalf("window = %d, want the new model's", ag.ContextWindow)
	}

	// A model the catalog does not describe is unknown, not the previous
	// model's limit: compaction is destructive and must not run on a borrowed
	// number.
	if a.slash(context.Background(), ag, "/model vendor/unlisted") {
		t.Fatal("/model must not exit the session")
	}
	if ag.ContextWindow != 0 {
		t.Fatalf("window = %d, want unknown for a model the catalog does not describe", ag.ContextWindow)
	}
}
