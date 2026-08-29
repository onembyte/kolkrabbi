package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/local"
	"github.com/onembyte/kolkrabbi/internal/tui"
)

func hostPickerApp(t *testing.T, connectorVerified *bool, accelerators int) (*app, *engine.Agent) {
	t.Helper()
	dirs := isolateConnectorState(t)
	storeFirstRunKey(t)
	if connectorVerified != nil {
		ollamaConnector(t, dirs, *connectorVerified)
	}
	a, _, _ := newTestApp(t, "")
	a.discoverHost = func(context.Context) local.Host {
		return local.Host{State: local.HostRunning, Addr: "127.0.0.1:11434", Version: "0.33.1"}
	}
	a.listHostModels = func(context.Context, string, string) ([]local.HostModel, error) {
		return []local.HostModel{
			{Name: "qwen2.5-coder:7b", Parameters: "7.6B", Quantization: "Q4_K_M", ContextLength: 32768, Tools: true, CapabilitiesKnown: true},
			{Name: "gemma2:9b", Parameters: "9.2B", Quantization: "Q4_K_M", ContextLength: 8192, CapabilitiesKnown: true},
			{Name: "gpt-oss:120b-cloud", Cloud: true, ContextLength: 131072, Tools: true, CapabilitiesKnown: true},
		}, nil
	}
	a.probeHardware = func(context.Context, string) local.Hardware {
		hw := local.Hardware{}
		for range accelerators {
			hw.Accelerators = append(hw.Accelerators, local.Accelerator{Vendor: "nvidia", Name: "test"})
		}
		return hw
	}
	agent, err := a.newAgent(context.Background(), &options{})
	if err != nil {
		t.Fatal(err)
	}
	return a, agent
}

func rowByID(rows []tui.ModelSpec, id string) (tui.ModelSpec, bool) {
	for _, row := range rows {
		if row.ID == id {
			return row, true
		}
	}
	return tui.ModelSpec{}, false
}

// E9. The picker lists what the host actually serves, under the ids the router
// understands. The old rows were a static catalogue of models nobody had
// pulled, under bare ids that went to the gateway and 404'd — a row that
// cannot be picked is a trap, not a row.
func TestPickerListsTheHostsModelsAndNotTheOldCatalogue(t *testing.T) {
	a, agent := hostPickerApp(t, nil, 0)
	rows := tuiModels(context.Background(), a, agent)

	row, ok := rowByID(rows, "ollama/qwen2.5-coder:7b")
	if !ok {
		t.Fatalf("no row for the pulled model; rows: %+v", rows)
	}
	if row.Cost != tui.CostLocal || !strings.Contains(row.Name, "7.6B") || !strings.Contains(row.Name, "CPU only") {
		t.Errorf("local row = %+v, want local cost, its size, and CPU only on a machine with no accelerator", row)
	}
	chatOnly, _ := rowByID(rows, "ollama/gemma2:9b")
	if !strings.Contains(chatOnly.Name, "chat only") {
		t.Errorf("a model without tools is not marked chat only: %+v", chatOnly)
	}
	if _, stale := rowByID(rows, "qwen2.5-coder:7b"); stale {
		t.Fatal("the old catalogue row is still listed under a bare id that the gateway will 404")
	}
}

func TestPickerSaysGPUWhenThereIsOne(t *testing.T) {
	a, agent := hostPickerApp(t, nil, 1)
	row, _ := rowByID(tuiModels(context.Background(), a, agent), "ollama/qwen2.5-coder:7b")
	if strings.Contains(row.Name, "CPU only") {
		t.Errorf("a machine with an accelerator was labelled CPU only: %+v", row)
	}
}

// A cloud model bills against the Ollama plan, so its row is the plan's:
// subscription when the connector is verified, sign-in-first when not.
func TestPickerLabelsCloudRowsByTheConnector(t *testing.T) {
	verified, unverified := true, false
	a, agent := hostPickerApp(t, &verified, 0)
	row, _ := rowByID(tuiModels(context.Background(), a, agent), "ollama/gpt-oss:120b-cloud")
	if row.Cost != tui.CostSubscription {
		t.Errorf("verified connector: cloud row cost = %q, want %q", row.Cost, tui.CostSubscription)
	}
	a, agent = hostPickerApp(t, &unverified, 0)
	row, _ = rowByID(tuiModels(context.Background(), a, agent), "ollama/gpt-oss:120b-cloud")
	if row.Cost != tui.CostSubscriptionLogin || !strings.Contains(row.Name, "kolk plans login ollama") {
		t.Errorf("unverified connector: cloud row = %+v, want sign-in-first with the command", row)
	}
	a, agent = hostPickerApp(t, nil, 0)
	row, _ = rowByID(tuiModels(context.Background(), a, agent), "ollama/gpt-oss:120b-cloud")
	if row.Cost != tui.CostSubscriptionLogin {
		t.Errorf("no connector: cloud row cost = %q, want sign-in-first", row.Cost)
	}
}

// The guard that matters. The engine sends tool schemas by mode, never by
// model, so a model without tools in code or agent mode fails with a 400 in
// the middle of a turn. Refusing at selection, with plan 06's sentence, is
// the difference between a choice and a surprise.
func TestAChatOnlyHostModelIsRefusedOutsideChatMode(t *testing.T) {
	a, agent := hostPickerApp(t, nil, 0)
	agent.Mode = engine.ModeCode
	_, err := a.switchModel(context.Background(), agent, "ollama/gemma2:9b")
	if err == nil {
		t.Fatal("a chat-only model was selected in code mode; the 400 comes mid-turn now")
	}
	if !strings.Contains(err.Error(), "no tool support") || !strings.Contains(err.Error(), "chat") {
		t.Errorf("refusal %q does not say why or what to do", err)
	}
	agent.Mode = engine.ModeChat
	if _, err := a.switchModel(context.Background(), agent, "ollama/gemma2:9b"); err != nil {
		t.Fatalf("chat mode refused a chat-only model: %v", err)
	}
	agent.Mode = engine.ModeCode
	if _, err := a.switchModel(context.Background(), agent, "ollama/qwen2.5-coder:7b"); err != nil {
		t.Fatalf("a tool-capable host model was refused in code mode: %v", err)
	}
}

// Never the default. The free-first chooser cannot tell a 1.5B local model
// from a 480B free gateway one; local is an explicit choice.
func TestAHostModelIsNeverTheDefault(t *testing.T) {
	_, agent := hostPickerApp(t, nil, 0)
	if strings.HasPrefix(agent.Model, "ollama/") {
		t.Fatalf("the session started on %q with nothing configured", agent.Model)
	}
}

// Merged from a34.6: an installed, idle Ollama draws from its manifest tree,
// and catalogued models not pulled carry the pull command — all under the
// ids the router understands, never a bare name that goes to the gateway.
func TestPickerDrawsAnIdleOllamaFromItsManifestTree(t *testing.T) {
	isolateConnectorState(t)
	storeFirstRunKey(t)
	a, _, _ := newTestApp(t, "")
	a.discoverHost = func(context.Context) local.Host { return local.Host{State: local.HostInstalled, Binary: "/opt/ollama"} }
	a.pulledNames = func() map[string]bool { return map[string]bool{"qwen2.5-coder": true} }
	agent, err := a.newAgent(context.Background(), &options{})
	if err != nil {
		t.Fatal(err)
	}
	rows := tuiModels(context.Background(), a, agent)
	pulledRow, ok := rowByID(rows, "ollama/qwen2.5-coder:7b")
	if !ok || !strings.Contains(pulledRow.Name, "runs on this machine") || !strings.Contains(pulledRow.Name, "starts ollama") {
		t.Fatalf("pulled model on an idle host = %+v, want a runnable row that says it starts ollama", pulledRow)
	}
	notPulled, ok := rowByID(rows, "ollama/llama3.1:8b")
	if !ok || !strings.Contains(notPulled.Name, "kolk localia pull llama3.1:8b") {
		t.Fatalf("unpulled catalogue model = %+v, want the pull command", notPulled)
	}
	for _, row := range rows {
		if row.Cost == tui.CostLocal && !strings.HasPrefix(row.ID, "ollama/") {
			t.Fatalf("a local row carries a bare id the gateway will 404: %+v", row)
		}
	}
}
