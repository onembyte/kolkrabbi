package engine

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

// The registry's first reader: a free model already known to be capped is not
// tried again during rotation. Without the registry, every 429 was news.
func TestFreeRotationSkipsACandidateKnownToBeCooling(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{StatusCode: http.StatusTooManyRequests, ErrorBody: `{"error":{"message":"rate limited"}}`},
		enginetest.Step{Text: "answer"},
	)
	defer srv.Close()
	agent, _, _, _ := newTestAgentInternal(t, srv, ModeCode)
	agent.FreeModels = []string{"vendor/one:free", "vendor/two:free", "vendor/three:free"}
	agent.SetSessionModel("vendor/one:free")
	dir := t.TempDir()
	agent.Cooldowns = OpenCooldowns(filepath.Join(dir, "s.cooldowns.json"), filepath.Join(dir, "shared.json"))
	// free/two hit a wall a moment ago, in this or another session.
	agent.Cooldowns.Mark(provider.Limit{Kind: provider.LimitEndpointCapacity, Scope: provider.ScopeModel, Connector: "openrouter", Model: "vendor/two:free", RetryAfter: 10 * time.Minute, Source: "status"})

	if err := agent.RunTurn(context.Background(), "hello"); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	if got := srv.Models; len(got) != 2 || got[0] != "vendor/one:free" || got[1] != "vendor/three:free" {
		t.Fatalf("models asked = %v, want one then three (two is cooling)", got)
	}
}
