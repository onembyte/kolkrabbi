package engine

import (
	"bufio"
	"context"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/session"
)

type recordingDecider struct {
	confirmation Confirmation
	ctx          context.Context
	allow        bool
	calls        int
}

func (d *recordingDecider) Confirm(ctx context.Context, confirmation Confirmation) bool {
	d.ctx = ctx
	d.confirmation = confirmation
	d.calls++
	return d.allow
}

func TestDeciderOwnsInteractiveConfirmationAheadOfTheLegacyReader(t *testing.T) {
	ctx := context.WithValue(context.Background(), deciderContextKey{}, "turn")
	decider := &recordingDecider{allow: true}
	ag := New(Options{
		Sess:    session.New(t.TempDir(), "model"),
		In:      bufio.NewReader(strings.NewReader("n\n")),
		Decider: decider,
	})

	if !ag.confirm(ctx, "Run shell command", "go test ./...") {
		t.Fatal("decider approval was ignored in favor of legacy stdin")
	}
	if decider.calls != 1 || decider.ctx != ctx {
		t.Fatalf("decider calls/context = %d/%v", decider.calls, decider.ctx)
	}
	want := Confirmation{Action: "Run shell command", Detail: "go test ./..."}
	if decider.confirmation != want {
		t.Fatalf("confirmation = %#v, want %#v", decider.confirmation, want)
	}
}

func TestYoloBypassesTheDecider(t *testing.T) {
	decider := &recordingDecider{}
	ag := New(Options{Sess: session.New(t.TempDir(), "model"), Yolo: true, Decider: decider})
	if !ag.confirm(context.Background(), "write", "file") {
		t.Fatal("yolo did not approve")
	}
	if decider.calls != 0 {
		t.Fatalf("yolo consulted the decider %d times", decider.calls)
	}
}

type deciderContextKey struct{}
