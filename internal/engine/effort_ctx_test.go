package engine

import (
	"context"
	"io"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

type effortCapturingBackend struct{ seen string }

func (b *effortCapturingBackend) StreamChat(ctx context.Context, _ string, _ []provider.Message, _ []provider.Tool, _ func(string)) (provider.Message, provider.Meta, error) {
	b.seen = provider.EffortFrom(ctx)
	return provider.Message{Role: "assistant", Content: "ok"}, provider.Meta{}, nil
}

// A turn carries the session's effort to the backend in its context, so a
// keyed vendor client can project it onto the vendor's word (V34.4c.1b).
func TestATurnCarriesTheSessionEffortToTheBackend(t *testing.T) {
	backend := &effortCapturingBackend{}
	a := New(Options{Backend: backend, Mode: ModeChat, Model: "vendor/x", Effort: EffortMax,
		Permission: PermissionFullAuto, Out: io.Discard, Sess: enginetest.NewFakeSession("s", "vendor/x")})
	if err := a.RunTurn(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if backend.seen != EffortMax {
		t.Fatalf("backend saw effort %q, want %q", backend.seen, EffortMax)
	}
	if err := a.SetEffort("ultra"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.fastLaneCall(context.Background(), "vendor/x", nil, nil); err != nil {
		t.Fatal(err)
	}
	if backend.seen != EffortUltra {
		t.Fatalf("fast lane saw effort %q, want %q", backend.seen, EffortUltra)
	}
}
