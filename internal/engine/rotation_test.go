package engine_test

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/session"
)

func TestFreeModel429AutoRotates(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{
			StatusCode: http.StatusTooManyRequests,
			ErrorBody:  `{"error":{"message":"Rate limited"}}`,
		},
		enginetest.Step{
			Text: "success from rotated free model",
		},
	)
	defer srv.Close()

	client := provider.NewCompatibleClient(srv.URL)

	var out bytes.Buffer
	ag := engine.New(engine.Options{
		Client:      client,
		Model:       "first/free-model:free",
		Mode:        engine.ModeChat,
		Out:         &out,
		Sess:        session.New(t.TempDir(), "first/free-model:free"),
		PinnedModel: false,
		FreeModels:  []string{"first/free-model:free", "second/free-model:free"},
		RetryWait:   func(context.Context, time.Duration) error { return nil },
	})

	err := ag.RunTurn(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Turn error = %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "rotating to second/free-model:free") {
		t.Fatalf("expected rotation message, got %q", got)
	}
	if ag.Model != "second/free-model:free" {
		t.Errorf("ag.Model = %s, want second/free-model:free", ag.Model)
	}
}

func TestPinnedModelNeverAutoRotatesOn429(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{StatusCode: http.StatusTooManyRequests, ErrorBody: `{"error":{"message":"Rate limited"}}`},
		enginetest.Step{StatusCode: http.StatusTooManyRequests, ErrorBody: `{"error":{"message":"Rate limited"}}`},
		enginetest.Step{StatusCode: http.StatusTooManyRequests, ErrorBody: `{"error":{"message":"Rate limited"}}`},
		enginetest.Step{StatusCode: http.StatusTooManyRequests, ErrorBody: `{"error":{"message":"Rate limited"}}`},
	)
	defer srv.Close()

	client := provider.NewCompatibleClient(srv.URL)

	var out bytes.Buffer
	ag := engine.New(engine.Options{
		Client:      client,
		Model:       "pinned/free-model:free",
		Mode:        engine.ModeChat,
		Out:         &out,
		Sess:        session.New(t.TempDir(), "pinned/free-model:free"),
		PinnedModel: true,
		FreeModels:  []string{"pinned/free-model:free", "second/free-model:free"},
		RetryWait:   func(context.Context, time.Duration) error { return nil },
	})

	err := ag.RunTurn(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error for pinned model 429 exhaustion, got nil")
	}

	got := out.String()
	if strings.Contains(got, "rotating to") {
		t.Fatalf("pinned model should NEVER auto-rotate, got %q", got)
	}
	if ag.Model != "pinned/free-model:free" {
		t.Errorf("ag.Model changed from pinned model: %s", ag.Model)
	}
}

func TestFreeModelRotationUsesEachCandidateOncePerTurn(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{StatusCode: http.StatusTooManyRequests, ErrorBody: `{"error":{"message":"Rate limited"}}`},
		enginetest.Step{StatusCode: http.StatusTooManyRequests, ErrorBody: `{"error":{"message":"Rate limited"}}`},
		enginetest.Step{StatusCode: http.StatusTooManyRequests, ErrorBody: `{"error":{"message":"Rate limited"}}`},
		enginetest.Step{Text: "recovered after exhausting alternatives"},
	)
	defer srv.Close()

	client := provider.NewCompatibleClient(srv.URL)
	var out bytes.Buffer
	ag := engine.New(engine.Options{
		Client:     client,
		Model:      "one/free:free",
		Mode:       engine.ModeChat,
		Out:        &out,
		Sess:       session.New(t.TempDir(), "one/free:free"),
		FreeModels: []string{"one/free:free", "two/free:free", "three/free:free"},
		RetryWait:  func(context.Context, time.Duration) error { return nil },
	})

	if err := ag.RunTurn(context.Background(), "hello"); err != nil {
		t.Fatalf("Turn error = %v", err)
	}

	for _, model := range []string{"two/free:free", "three/free:free"} {
		if strings.Count(out.String(), "rotating to "+model) != 1 {
			t.Fatalf("expected one rotation to %s, output %q", model, out.String())
		}
	}

	if ag.Model != "three/free:free" {
		t.Fatalf("final model = %s, want three/free:free", ag.Model)
	}
}
