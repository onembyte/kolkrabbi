package engine_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/session"
)

func TestFastLaneModelSelection(t *testing.T) {
	// 1. Free session model -> Fast lane uses free model
	freeAg := engine.New(engine.Options{
		Model: "meta-llama/llama-3.3-70b-instruct:free",
	})
	if got := freeAg.FastLaneModel(); !provider.ModelIsFree(provider.ModelInfo{ID: got}) {
		t.Errorf("FastLaneModel for free session = %q, want a free model", got)
	}

	// 2. Paid session model -> Fast lane uses cheap high-throughput model (e.g. gemini-2.5-flash)
	paidAg := engine.New(engine.Options{
		Model: "anthropic/claude-3-7-sonnet",
	})
	if got := paidAg.FastLaneModel(); got != "google/gemini-2.5-flash" {
		t.Errorf("FastLaneModel for paid session = %q, want google/gemini-2.5-flash", got)
	}
}

func TestFastLaneChatExecutesIsolatedWithoutTools(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{
			Text: "Refactor auth middleware to use bearer token",
		},
	)
	defer srv.Close()

	client := provider.NewCompatibleClient(srv.URL)

	ag := engine.New(engine.Options{
		Client: client,
		Model:  "anthropic/claude-3-7-sonnet",
		Sess:   session.New(t.TempDir(), "anthropic/claude-3-7-sonnet"),
		Out:    io.Discard,
	})

	initialCount := len(ag.Sess.GetMessages())

	// Run fast lane auxiliary call
	resp, err := ag.FastLaneChat(context.Background(), "You are a titler", "Summarize this request: please refactor auth")
	if err != nil {
		t.Fatalf("FastLaneChat failed: %v", err)
	}

	if !strings.Contains(resp, "Refactor auth middleware") {
		t.Fatalf("FastLaneChat response = %q", resp)
	}

	// Session history must remain pristine (0 messages added by fast lane)
	if len(ag.Sess.GetMessages()) != initialCount {
		t.Fatalf("FastLaneChat leaked into session turns: %d messages found, want %d", len(ag.Sess.GetMessages()), initialCount)
	}
}

// The fast lane on a vendor-ladder session must be a rung of that vendor. Live
// on 2026-09-03 the saga planner on a Claude Max session asked the Claude child
// for the best discovered free gateway model; the child answered as Fable and
// the catalog recorded a gateway id as a verified Claude model. The planner's
// cheap judgement belongs on the cheapest signed-in rung, or on the session
// model when nothing cheaper is signed in — never on a model the child cannot
// run. A gateway session keeps the free pick.
func TestFastLaneOnAVendorSessionIsARungOfThatVendor(t *testing.T) {
	onPlan := enginetest.NewFakeSession("s", "claude-fable")
	onPlan.SetConnector("claude") // what startup records for a plan session
	agent := &engine.Agent{Options: engine.Options{Model: "claude-fable", Sess: onPlan, FreeModels: []string{"cohere/north-mini-code:free"}}}
	agent.RungAvailable = func(vendor, model string) bool { return vendor == "claude" && model == "claude-haiku" }
	if got := agent.FastLaneModel(); got != "claude-haiku" {
		t.Fatalf("fast lane on fable with haiku signed in = %q, want claude-haiku", got)
	}
	agent.RungAvailable = nil
	if got := agent.FastLaneModel(); got != "claude-fable" {
		t.Fatalf("fast lane on fable with nothing cheaper = %q, want the session model", got)
	}
	// The same Claude model reached through the gateway: no connector, so the
	// backend is the gateway and the free pick is right.
	viaGateway := enginetest.NewFakeSession("s", "anthropic/claude-opus-5")
	gateway := &engine.Agent{Options: engine.Options{Model: "anthropic/claude-opus-5", Sess: viaGateway, FreeModels: []string{"cohere/north-mini-code:free"}}}
	if got := gateway.FastLaneModel(); got != "cohere/north-mini-code:free" {
		t.Fatalf("fast lane on a gateway session = %q, want the discovered free model", got)
	}
}
