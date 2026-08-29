package cli

import (
	"context"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/local"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

type warmingRoute struct {
	warmed []string
}

func (r *warmingRoute) StreamChat(context.Context, string, []provider.Message, []provider.Tool, func(string)) (provider.Message, provider.Meta, error) {
	return provider.Message{}, provider.Meta{}, nil
}
func (r *warmingRoute) Warm(_ context.Context, model string) { r.warmed = append(r.warmed, model) }

// E8. Choosing a host model loads it ahead of the first turn, with the route
// prefix stripped — the server has never heard of "ollama/". A gateway model
// warms nothing: there is nothing on this machine to load.
func TestSelectingAHostModelWarmsItByItsWireName(t *testing.T) {
	storeFirstRunKey(t)
	a, _, _ := newTestApp(t, "")
	a.discoverHost = func(context.Context) local.Host { return local.Host{State: local.HostRunning, Addr: "127.0.0.1:11434"} }
	route := &warmingRoute{}
	a.warmHost = func(ctx context.Context, w modelWarmer, model string) { w.Warm(ctx, model) }
	agent, err := a.newAgent(context.Background(), &options{})
	if err != nil {
		t.Fatal(err)
	}
	agent.Routes = map[string]engine.ChatBackend{"ollama": route}

	if _, err := a.switchModel(context.Background(), agent, "ollama/qwen2.5-coder:7b"); err != nil {
		t.Fatal(err)
	}
	if len(route.warmed) != 1 || route.warmed[0] != "qwen2.5-coder:7b" {
		t.Fatalf("warmed %v, want the wire name once", route.warmed)
	}
	if _, err := a.switchModel(context.Background(), agent, "openai/gpt-5.6-luna"); err != nil {
		t.Fatal(err)
	}
	if len(route.warmed) != 1 {
		t.Fatalf("a gateway model warmed the host route: %v", route.warmed)
	}
}
