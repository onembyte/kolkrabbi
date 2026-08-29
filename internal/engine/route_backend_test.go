package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// routeBackend answers everything and remembers the model ids it was asked
// for, which is the only thing a routing test needs to see.
type routeBackend struct {
	name   string
	models []string
}

func (b *routeBackend) StreamChat(_ context.Context, model string, _ []provider.Message, _ []provider.Tool, _ func(string)) (provider.Message, provider.Meta, error) {
	b.models = append(b.models, model)
	return provider.Message{Role: "assistant", Content: "from " + b.name}, provider.Meta{}, nil
}

func routedAgent(gateway ChatBackend, routes map[string]ChatBackend) *Agent {
	return &Agent{Options: Options{
		Backend: gateway,
		Routes:  routes,
		Out:     &strings.Builder{},
	}}
}

// Every OpenRouter id has the shape vendor/model. The router must leave those
// alone: a prefix it does not own is not a route, it is a vendor.
func TestBackendForPassesGatewayIDsThroughUntouched(t *testing.T) {
	gateway := &routeBackend{name: "gateway"}
	a := routedAgent(gateway, map[string]ChatBackend{"ollama": &routeBackend{name: "ollama"}})
	for _, id := range []string{"openai/gpt-5.6-luna", "meta-llama/llama-3:free", "openrouter/free", "qwen/qwen3-coder"} {
		backend, wire, err := a.backendFor(id)
		if err != nil {
			t.Fatalf("backendFor(%q): %v", id, err)
		}
		if backend != ChatBackend(gateway) || wire != id {
			t.Errorf("backendFor(%q) = (%v, %q), want the gateway with the id untouched", id, backend, wire)
		}
	}
}

func TestBackendForStripsAnOwnedPrefixForARegisteredRoute(t *testing.T) {
	ollama := &routeBackend{name: "ollama"}
	a := routedAgent(&routeBackend{name: "gateway"}, map[string]ChatBackend{"ollama": ollama})
	backend, wire, err := a.backendFor("ollama/qwen2.5-coder:7b")
	if err != nil {
		t.Fatal(err)
	}
	if backend != ChatBackend(ollama) {
		t.Fatalf("routed to %v, want the ollama backend", backend)
	}
	if wire != "qwen2.5-coder:7b" {
		t.Fatalf("wire id = %q, want the prefix stripped: the server has never heard of it", wire)
	}
}

// The guard that matters. A host id with no server behind it must never reach
// the gateway: the prefix would be a 404 about a model the user never named,
// and worse, a gateway that happened to know the id would answer for money.
func TestAnOwnedPrefixWithNoRouteIsRefusedNotForwarded(t *testing.T) {
	gateway := &routeBackend{name: "gateway"}
	a := routedAgent(gateway, nil)

	_, _, err := a.streamChat(context.Background(), "reply", "ollama/qwen2.5-coder:7b", nil, nil, nil)
	if err == nil {
		t.Fatal("a host id with no route was answered, want a refusal")
	}
	if !strings.Contains(err.Error(), "ollama/qwen2.5-coder:7b") || !strings.Contains(err.Error(), "Ollama") {
		t.Errorf("refusal %q names neither the id nor what is missing", err)
	}
	if len(gateway.models) != 0 {
		t.Fatalf("the gateway was asked for %v; a host id must never be forwarded", gateway.models)
	}
}

func TestARoutedTurnReachesItsBackendWithTheWireID(t *testing.T) {
	gateway := &routeBackend{name: "gateway"}
	ollama := &routeBackend{name: "ollama"}
	a := routedAgent(gateway, map[string]ChatBackend{"ollama": ollama})

	msg, _, err := a.streamChat(context.Background(), "reply", "ollama/qwen2.5-coder:7b", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content != "from ollama" {
		t.Fatalf("answer %q did not come from the routed backend", msg.Content)
	}
	if len(ollama.models) != 1 || ollama.models[0] != "qwen2.5-coder:7b" {
		t.Fatalf("ollama saw %v, want exactly the wire id once", ollama.models)
	}
	if len(gateway.models) != 0 {
		t.Fatalf("the gateway was also asked (%v); one turn, one backend", gateway.models)
	}
}

// moveToMetered swaps a.Backend back to the gateway client. Routes are not
// a.Backend and must survive that swap, or a limit on the plan model would
// silently make every host model unreachable.
func TestMovingToMeteredLeavesRoutesAlone(t *testing.T) {
	ollama := &routeBackend{name: "ollama"}
	a := routedAgent(&routeBackend{name: "plan"}, map[string]ChatBackend{"ollama": ollama})
	a.Client = provider.NewClient("k")
	a.moveToMetered("openai/gpt-5.6-luna")

	backend, _, err := a.backendFor("ollama/qwen2.5-coder:7b")
	if err != nil {
		t.Fatal(err)
	}
	if backend != ChatBackend(ollama) {
		t.Fatal("the ollama route was lost when the session moved to a metered model")
	}
}

// The fast lane calls the backend directly rather than through the turn's
// retry path, so it needs the router too — a host model chosen for the lane
// otherwise goes to the gateway with a prefix the gateway has never seen.
func TestTheFastLaneRoutesAsWell(t *testing.T) {
	gateway := &routeBackend{name: "gateway"}
	ollama := &routeBackend{name: "ollama"}
	a := routedAgent(gateway, map[string]ChatBackend{"ollama": ollama})

	msg, _, err := a.fastLaneCall(context.Background(), "ollama/qwen2.5-coder:7b", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content != "from ollama" || len(gateway.models) != 0 {
		t.Fatalf("fast lane answered %q with gateway calls %v; want the routed backend alone", msg.Content, gateway.models)
	}
}
