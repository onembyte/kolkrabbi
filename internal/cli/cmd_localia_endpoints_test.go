package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/local"
)

// stubIdentify answers for an address without a network.
func stubIdentify(a *app, found map[string]local.Runtime) {
	a.identify = func(_ context.Context, addr string) (local.Runtime, error) {
		if runtime, ok := found[addr]; ok {
			return runtime, nil
		}
		return local.Runtime{}, errors.New("nothing that serves models answered at " + addr)
	}
}

func endpointsOf(t *testing.T, a *app) []config.Endpoint {
	t.Helper()
	dirs, err := a.resolve()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(dirs.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Local.Endpoints
}

// Adding an endpoint probes it, says what is there, and saves it. A name
// already taken, a name kolk reserves, and an address that answers nothing
// are all refused with nothing written.
func TestLocaliaAddProbesAndSaves(t *testing.T) {
	a, out, _ := newTestApp(t, "")
	stubIdentify(a, map[string]local.Runtime{
		"192.168.1.50:11434": {Kind: local.KindOllama, Base: "http://192.168.1.50:11434/v1", Version: "0.5.7", Models: []string{"qwen3", "llama3"}},
	})
	if err := a.runLocalia(context.Background(), []string{"add", "shop", "192.168.1.50:11434"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"shop", "ollama", "0.5.7", "qwen3", "shop/qwen3"} {
		if !strings.Contains(got, want) {
			t.Errorf("add did not report %q:\n%s", want, got)
		}
	}
	saved := endpointsOf(t, a)
	if len(saved) != 1 || saved[0].Name != "shop" || saved[0].Kind != local.KindOllama || saved[0].Base != "http://192.168.1.50:11434/v1" {
		t.Fatalf("saved = %+v", saved)
	}

	if err := a.runLocalia(context.Background(), []string{"add", "shop", "192.168.1.50:11434"}); err == nil {
		t.Error("a name already taken was accepted")
	}
	if err := a.runLocalia(context.Background(), []string{"add", "ollama", "192.168.1.50:11434"}); err == nil {
		t.Error("a reserved name was accepted")
	}
	if err := a.runLocalia(context.Background(), []string{"add", "rig", "10.0.0.9:11434"}); err == nil {
		t.Error("an address that answers nothing was accepted")
	}
	if len(endpointsOf(t, a)) != 1 {
		t.Fatalf("a refused add still wrote: %+v", endpointsOf(t, a))
	}
}

// An address that is not this machine gets one plain sentence about what
// will leave it, before it is saved. Loopback gets none: nothing leaves.
func TestLocaliaSaysWhatLeavesTheMachine(t *testing.T) {
	for _, tc := range []struct {
		name, addr string
		warned     bool
	}{
		{"shop", "192.168.1.50:11434", true},
		{"rig", "100.64.0.7:11434", true},
		{"box", "127.0.0.1:12434", false},
		{"named", "localhost:12434", false},
	} {
		a, out, _ := newTestApp(t, "")
		stubIdentify(a, map[string]local.Runtime{tc.addr: {Kind: local.KindOpenAI, Base: "http://" + tc.addr + "/engines/v1", Models: []string{"qwen3"}}})
		if err := a.runLocalia(context.Background(), []string{"add", tc.name, tc.addr}); err != nil {
			t.Fatal(err)
		}
		said := strings.Contains(out.String(), "leave this machine")
		if said != tc.warned {
			t.Errorf("%s: warned = %v, want %v:\n%s", tc.addr, said, tc.warned, out.String())
		}
	}
}

// The endpoints are listed with what is at each, and one can be forgotten.
func TestLocaliaListsAndForgetsEndpoints(t *testing.T) {
	a, out, _ := newTestApp(t, "")
	stubIdentify(a, map[string]local.Runtime{
		"192.168.1.50:11434": {Kind: local.KindOllama, Base: "http://192.168.1.50:11434/v1", Models: []string{"qwen3"}},
		"100.64.0.7:12434":   {Kind: local.KindOpenAI, Base: "http://100.64.0.7:12434/engines/v1", Models: []string{"gemma"}},
	})
	for _, add := range [][]string{{"add", "shop", "192.168.1.50:11434"}, {"add", "rig", "100.64.0.7:12434"}} {
		if err := a.runLocalia(context.Background(), add); err != nil {
			t.Fatal(err)
		}
	}
	out.Reset()
	if err := a.runLocalia(context.Background(), []string{"list"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"shop", "192.168.1.50:11434", "rig", "100.64.0.7:12434"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("list lacks %q:\n%s", want, out.String())
		}
	}
	if err := a.runLocalia(context.Background(), []string{"rm", "shop"}); err != nil {
		t.Fatal(err)
	}
	saved := endpointsOf(t, a)
	if len(saved) != 1 || saved[0].Name != "rig" {
		t.Fatalf("after rm: %+v", saved)
	}
	if err := a.runLocalia(context.Background(), []string{"rm", "shop"}); err == nil {
		t.Error("forgetting a name that is not there was accepted")
	}
}

// Using an endpoint points this session at it. With no model named, the
// models it serves are listed instead of a guess being made.
func TestLocaliaUsePointsTheSessionAtTheEndpoint(t *testing.T) {
	a, out, _ := newTestApp(t, "")
	stubIdentify(a, map[string]local.Runtime{
		"192.168.1.50:11434": {Kind: local.KindOllama, Base: "http://192.168.1.50:11434/v1", Models: []string{"qwen3", "llama3"}},
	})
	if err := a.runLocalia(context.Background(), []string{"add", "shop", "192.168.1.50:11434"}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := a.runLocalia(context.Background(), []string{"use", "shop"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"qwen3", "llama3", "/localia use shop"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("use with no model did not offer what it serves (%q):\n%s", want, out.String())
		}
	}
	if err := a.runLocalia(context.Background(), []string{"use", "nowhere"}); err == nil {
		t.Error("using an endpoint that was never added was accepted")
	}
}

// A configured endpoint answers for its own name: the model-id prefix is
// the endpoint, which is what makes `/model shop/qwen3` reach the shop box.
func TestEndpointRoutesAreKeyedByName(t *testing.T) {
	cfg := &config.Config{}
	cfg.PutEndpoint(config.Endpoint{Name: "shop", Addr: "192.168.1.50:11434", Kind: local.KindOllama, Base: "http://192.168.1.50:11434/v1"})
	cfg.PutEndpoint(config.Endpoint{Name: "rig", Addr: "100.64.0.7:12434", Kind: local.KindOpenAI, Base: "http://100.64.0.7:12434/engines/v1"})
	cfg.PutEndpoint(config.Endpoint{Name: "half", Addr: "10.0.0.2:1", Kind: local.KindOpenAI})
	routes := endpointRoutes(cfg)
	if len(routes) != 2 || routes["shop"] == nil || routes["rig"] == nil {
		t.Fatalf("routes = %v, want one per endpoint that has a base", routes)
	}
}

// With a session running, `use <name> <model>` switches it in place: the
// point of the command is not having to run a second one.
func TestLocaliaUseSwitchesTheRunningSession(t *testing.T) {
	a, ag, out := replFixture(t, "")
	stubIdentify(a, map[string]local.Runtime{
		"192.168.1.50:11434": {Kind: local.KindOllama, Base: "http://192.168.1.50:11434/v1", Models: []string{"qwen3"}},
	})
	if err := a.runLocalia(context.Background(), []string{"add", "shop", "192.168.1.50:11434"}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := a.runLocaliaWith(context.Background(), ag, []string{"use", "shop", "qwen3"}); err != nil {
		t.Fatal(err)
	}
	if got := ag.SessionModel(); got != "shop/qwen3" {
		t.Fatalf("session model = %q, want shop/qwen3", got)
	}
	if !strings.Contains(out.String(), "shop/qwen3") {
		t.Fatalf("use did not say what it switched to:\n%s", out.String())
	}
}
