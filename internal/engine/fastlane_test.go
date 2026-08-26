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

	client := provider.NewClient("test-key")
	client.BaseURL = srv.URL

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
