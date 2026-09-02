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

// A session that ran on a subscription remembers the vendor conversation it
// was in, and the resume continues that conversation instead of opening an
// empty one — the context the previous process had does not vanish because
// Kolkrabbi restarted.
func TestResumeCarriesTheVendorConversationHandle(t *testing.T) {
	dirs := storeFirstRunKey(t)
	if err := dirs.EnsureData(); err != nil {
		t.Fatal(err)
	}
	enablePlanConnector(t, dirs)
	resumed := session.New(dirs.Sessions(), "claude-opus")
	resumed.SetEffort("high")
	resumed.SetConnector("claude")
	resumed.SetProviderStateName("vendor-conv-carried")
	if err := resumed.Save(); err != nil {
		t.Fatal(err)
	}

	a, _, _ := newTestApp(t, "")
	ag, err := a.newAgent(context.Background(), &options{session: resumed.ID})
	if err != nil {
		t.Fatal(err)
	}
	wrapped, ok := ag.Backend.(*verifyingBackend)
	if !ok {
		t.Fatalf("backend = %T, want the verifying wrapper", ag.Backend)
	}
	inner, ok := wrapped.inner.(*agentcli.ClaudeBackend)
	if !ok {
		t.Fatalf("inner = %T, want the claude backend", wrapped.inner)
	}
	if handle := inner.ProviderHandle(); handle != "vendor-conv-carried" {
		t.Fatalf("ProviderHandle() = %q, want the stored vendor conversation", handle)
	}
}

// A model switch mid-session resumes the same vendor conversation on the new
// model, and new provider state lands back in the session file.
func TestSwitchingPlanModelsCarriesTheVendorConversationHandle(t *testing.T) {
	dirs := storeFirstRunKey(t)
	if err := dirs.EnsureData(); err != nil {
		t.Fatal(err)
	}
	enablePlanConnector(t, dirs)
	stored := session.New(dirs.Sessions(), "claude-opus")
	stored.SetProviderStateName("vendor-conv-carried")
	if err := stored.Save(); err != nil {
		t.Fatal(err)
	}

	a, _, _ := newTestApp(t, "")
	ag, err := a.newAgent(context.Background(), &options{session: stored.ID, model: "vendor/model"})
	if err != nil {
		t.Fatal(err)
	}

	label, err := a.switchModel(context.Background(), ag, "claude-opus")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(label, "Claude Max") {
		t.Fatalf("switch label = %q, want the plan named", label)
	}
	wrapped, ok := ag.Backend.(*verifyingBackend)
	if !ok {
		t.Fatalf("backend = %T, want the verifying wrapper", ag.Backend)
	}
	inner, ok := wrapped.inner.(*agentcli.ClaudeBackend)
	if !ok {
		t.Fatalf("inner = %T, want the claude backend", wrapped.inner)
	}
	if handle := inner.ProviderHandle(); handle != "vendor-conv-carried" {
		t.Fatalf("ProviderHandle() = %q, want the same vendor conversation across the switch", handle)
	}
	if got := ag.Sess.ConnectorName(); got != "claude" {
		t.Fatalf("connector = %q, want claude after the switch", got)
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
	if !strings.Contains(err.Error(), `/plans login anthropic "Claude Max"`) {
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

// Codex has an adapter now, so the session's remaining reachable refusal for an
// enabled connector is the one the plan catalog itself carries: gemini, whose
// subscription may never be driven by a third party and is marked that way
// upstream of any adapter. A signed-in connector that kolk cannot drive must
// still say so instead of silently answering from OpenRouter.
func TestSessionRefusesAPlanModelNoAdapterCanServe(t *testing.T) {
	dirs := storeFirstRunKey(t)
	if err := provider.SaveConnector(context.Background(), dirs.ConnectorsFile(), provider.Connector{
		Provider: "google", Plan: "Google AI Pro", Name: "gemini",
		LoginOwner: "provider-cli", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	a, _, _ := newTestApp(t, "")

	_, err := a.newAgent(context.Background(), &options{model: "gemini-2.5-pro"})
	if err == nil {
		t.Fatal("a plan kolk cannot serve must say so instead of silently using OpenRouter")
	}
	if !strings.Contains(err.Error(), "unsupported subscription") {
		t.Fatalf("error = %v, want the catalog's reason rather than a silent provider swap", err)
	}
}

// Switching models inside a session has to switch the provider with them.
// Otherwise the status bar names one model while another provider answers.
func TestSlashModelSwitchesOntoAndOffAPlanBackend(t *testing.T) {
	dirs := isolateConnectorState(t)
	enablePlanConnector(t, dirs)
	signInAs(t, dirs, "openai", "ChatGPT Plus", "codex")
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

	if a.slash(context.Background(), ag, "/model gpt-plus") {
		t.Fatal("friendly /model alias must not exit the session")
	}
	if ag.Model != "gpt-5.6-sol" {
		t.Fatalf("friendly alias model = %q, want gpt-5.6-sol", ag.Model)
	}
	if _, ok := ag.Backend.(*verifyingBackend); !ok {
		t.Fatalf("backend = %T after the friendly plan alias", ag.Backend)
	}
	if !strings.Contains(out.String(), "ChatGPT Plus") {
		t.Fatalf("friendly alias output omitted the plan name: %q", out.String())
	}
}

func TestSlashModelSelectsEveryGPT56TierThroughThePlanAlias(t *testing.T) {
	tests := []struct {
		plan, alias, model string
	}{
		{"ChatGPT Plus", "gpt-plus-sol", "gpt-5.6-sol"},
		{"ChatGPT Plus", "gpt-plus-terra", "gpt-5.6-terra"},
		{"ChatGPT Plus", "gpt-plus-luna", "gpt-5.6-luna"},
		{"ChatGPT Pro", "gpt-pro-sol", "gpt-5.6-sol"},
		{"ChatGPT Pro", "gpt-pro-terra", "gpt-5.6-terra"},
		{"ChatGPT Pro", "gpt-pro-luna", "gpt-5.6-luna"},
	}
	for _, test := range tests {
		t.Run(test.alias, func(t *testing.T) {
			dirs := isolateConnectorState(t)
			signInAs(t, dirs, "openai", test.plan, "codex")
			a, ag, out := replFixture(t, "")
			if a.slash(context.Background(), ag, "/model "+test.alias) {
				t.Fatal("/model must not exit the session")
			}
			if ag.Model != test.model {
				t.Fatalf("alias %s selected model %q, want %q", test.alias, ag.Model, test.model)
			}
			if _, ok := ag.Backend.(*verifyingBackend); !ok {
				t.Fatalf("alias %s did not select a Codex plan backend", test.alias)
			}
			if !strings.Contains(out.String(), test.plan) {
				t.Fatalf("alias %s output omitted %s: %q", test.alias, test.plan, out.String())
			}
		})
	}
}

func TestSlashModelSelectsCanonicalSharedIDsOnEveryOpenAIPlan(t *testing.T) {
	for _, plan := range []string{"ChatGPT Plus", "ChatGPT Pro"} {
		for _, model := range []string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"} {
			t.Run(plan+"/"+model, func(t *testing.T) {
				dirs := isolateConnectorState(t)
				signInAs(t, dirs, "openai", plan, "codex")
				a, ag, out := replFixture(t, "")

				if a.slash(context.Background(), ag, "/model "+model) {
					t.Fatal("canonical /model must not exit the session")
				}
				if ag.Model != model {
					t.Fatalf("selected model = %q, want %q", ag.Model, model)
				}
				if _, ok := ag.Backend.(*verifyingBackend); !ok {
					t.Fatalf("canonical %s did not select a Codex plan backend", model)
				}
				if !strings.Contains(out.String(), plan) {
					t.Fatalf("canonical %s output omitted %s: %q", model, plan, out.String())
				}
			})
		}
	}
}

func TestSlashModelPickerCommandAppliesPlanEffort(t *testing.T) {
	dirs := isolateConnectorState(t)
	enablePlanConnector(t, dirs)
	a, ag, out := replFixture(t, "")

	if a.slash(context.Background(), ag, "/model claude-opus high") {
		t.Fatal("the picker's model command must not exit the session")
	}
	if ag.Model != "claude-opus" || ag.Effort != "high" {
		t.Fatalf("picker selection = model %q, effort %q; want claude-opus/high", ag.Model, ag.Effort)
	}
	if got := ag.Sess.SessionEffort(); got != "high" {
		t.Fatalf("session effort = %q, want the picker's high", got)
	}
	wrapped, ok := ag.Backend.(*verifyingBackend)
	if !ok {
		t.Fatalf("backend = %T, want the plan backend", ag.Backend)
	}
	inner, ok := wrapped.inner.(*agentcli.ClaudeBackend)
	if !ok || inner.Effort != "high" {
		t.Fatalf("inner backend = %#v, want Claude at high effort", wrapped.inner)
	}
	if !strings.Contains(out.String(), "high") {
		t.Fatalf("picker effort was not reported: %q", out.String())
	}
}

func TestSlashModelRefusesAnUnusablePlanModelWithoutChangingTheSession(t *testing.T) {
	isolateConnectorState(t)
	a, ag, out := replFixture(t, "")
	before, beforeBackend := ag.Model, ag.Backend

	if a.slash(context.Background(), ag, "/model claude-opus") {
		t.Fatal("/model must not exit the session")
	}
	if !strings.Contains(out.String(), `/plans login anthropic "Claude Max"`) {
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

// Mode is part of the vendor's spawn contract: kolk's chat mode runs the
// provider with no tool in context at all, so "chat cannot touch your files"
// is what the process is, not a prompt instruction. A mode change must
// therefore restart the provider process, exactly as an effort change does.
func TestSlashModeRestartsThePlanProviderInChatMode(t *testing.T) {
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

	if a.slash(context.Background(), ag, "/mode chat") {
		t.Fatal("/mode must not exit the session")
	}
	wrapped, ok := ag.Backend.(*verifyingBackend)
	if !ok {
		t.Fatalf("backend = %T, want the verifying wrapper after /mode", ag.Backend)
	}
	if ag.Backend == before {
		t.Fatal("/mode must replace a plan provider still running in the old mode")
	}
	if wrapped.mode != "chat" {
		t.Fatalf("provider mode = %q, want chat", wrapped.mode)
	}
	inner, ok := wrapped.inner.(*agentcli.ClaudeBackend)
	if !ok {
		t.Fatalf("inner = %T, want the claude backend", wrapped.inner)
	}
	if inner.Mode != "chat" {
		t.Fatalf("claude backend mode = %q, want chat", inner.Mode)
	}
	if !strings.Contains(out.String(), "restarted in chat mode") {
		t.Fatalf("output = %q, want it to say the provider restarted", out.String())
	}
}

// Agent mode is accepted on a plan provider, and the process restarts into it.
//
// This test asserted the opposite until 2026-08-30. The refusal's reason — the
// vendor schedules its own subagents kolk cannot record or stop — was true of
// the vendor's Task tool, which has never been in claudeCodeTools. kolk's agent
// mode schedules kolk's own children, each a process kolk starts and can stop.
// The refusal was broader than its own argument.
func TestSlashModeAcceptsAgentOnAPlanProvider(t *testing.T) {
	dirs := isolateConnectorState(t)
	enablePlanConnector(t, dirs)
	a, ag, out := replFixture(t, "")
	if a.slash(context.Background(), ag, "/model claude-opus") {
		t.Fatal("/model must not exit the session")
	}

	if a.slash(context.Background(), ag, "/mode agent") {
		t.Fatal("/mode must not exit the session")
	}
	if ag.Mode != "agent" {
		t.Fatalf("mode = %q, want agent", ag.Mode)
	}
	// A vendor process replays no argv, so a mode change means a new process.
	if !strings.Contains(out.String(), "restarted in agent mode") {
		t.Fatalf("output = %q, want the provider restarted into agent mode", out.String())
	}
	if !strings.Contains(out.String(), "agent lane:") {
		t.Fatalf("output = %q, want the plan-backed agent lane announcement", out.String())
	}
}

// A session can START in agent mode on a plan model, not only switch into it.
// The two paths share claudeModeFlags, so they were refused together and are
// allowed together.
func TestASessionCanStartInAgentModeOnAPlanModel(t *testing.T) {
	dirs := storeFirstRunKey(t)
	if err := dirs.EnsureData(); err != nil {
		t.Fatal(err)
	}
	enablePlanConnector(t, dirs)
	stored := session.New(dirs.Sessions(), "vendor/model")
	if err := stored.Save(); err != nil {
		t.Fatal(err)
	}

	a, _, _ := newTestApp(t, "")
	agent, err := a.newAgent(context.Background(), &options{session: stored.ID, model: "claude-opus", mode: "agent"})
	if err != nil {
		t.Fatalf("agent mode on a plan model was refused: %v", err)
	}
	if agent.Mode != "agent" {
		t.Errorf("mode = %q, want agent", agent.Mode)
	}
}

// Startup agent mode must announce the same spending lane as an in-session
// transition. Otherwise the user gets the limit information only after typing
// /mode agent, despite both paths creating the same kind of run.
func TestStartupAgentModeOnAPlanModelReportsTheAgentLane(t *testing.T) {
	storeFirstRunKey(t)
	dirs := isolateHome(t)
	enablePlanConnector(t, dirs)
	a, out, errOut := newTestApp(t, "")

	if code := a.main(context.Background(), []string{"--model", "claude-opus", "--mode", "agent"}); code != ExitOK {
		t.Fatalf("startup in agent mode exit = %d, stderr:\n%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "agent lane:") {
		t.Fatalf("output = %q, want the startup agent lane announcement", out.String())
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

// At the top rung with nothing signed in, the lane used to be silent — the
// user "chose" Fable, so there was nothing to say. But a Fable session that
// could run its commits on Haiku, and does not because no connector is signed
// in, is a saving the user does not know is on the table. Name it, and only it.
func TestTopRungLaneSaysWhatASignInWouldUnlock(t *testing.T) {
	isolateConnectorState(t)
	a, ag, out := replFixture(t, "")
	ag.SetSessionModel("claude-fable")
	ag.Mode = engine.ModeAgent

	a.reportAgentLane(ag)
	got := out.String()
	if !strings.Contains(got, "claude-fable only") || !strings.Contains(got, "/plans login") || !strings.Contains(got, "claude-haiku") {
		t.Fatalf("top-rung lane = %q, want the sign-in hint naming the cheapest rung", got)
	}
	if strings.Contains(got, "out of reach") {
		t.Fatalf("top-rung lane claimed something is capped: %q", got)
	}

	// An unranked model still says nothing: that would be a claim kolk cannot make.
	out.Reset()
	ag.SetSessionModel("mock/model")
	a.reportAgentLane(ag)
	if out.Len() != 0 {
		t.Fatalf("unranked model produced a lane line: %q", out.String())
	}
}
