package engine

import (
	"bufio"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/bus"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/tools"
	"github.com/onembyte/kolkrabbi/internal/xid"
	"github.com/onembyte/kolkrabbi/protocol"
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
		Sess:    enginetest.NewFakeSession("s_1", "model"),
		In:      bufio.NewReader(strings.NewReader("n\n")),
		Decider: decider,
	})

	if !firstOf(ag.confirm(ctx, Confirmation{Action: "Run shell command", Detail: "go test ./..."})) {
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

// confirm has no bypass of its own any more. Whether to ask is decided in one
// place, by the permission tier, so there is no second path that can quietly
// disagree with the first.
func TestConfirmAlwaysAsksTheDecider(t *testing.T) {
	decider := &recordingDecider{}
	ag := New(Options{Sess: enginetest.NewFakeSession("s_1", "model"), Permission: PermissionFullAuto, Decider: decider})

	firstOf(ag.confirm(context.Background(), Confirmation{Action: "write", Detail: "file"}))

	if decider.calls != 1 {
		t.Fatalf("confirm consulted the decider %d times, want exactly one", decider.calls)
	}
}

func TestFullAutoNeverReachesTheDecider(t *testing.T) {
	decider := &recordingDecider{}
	ag := New(Options{
		Sess:       enginetest.NewFakeSession("s_1", "model"),
		Permission: PermissionFullAuto, Decider: decider, Root: "/p", Out: io.Discard,
	})

	if !ag.guard(context.Background())(tools.Request{Tool: "bash", Command: "go test ./..."}) {
		t.Fatal("full-auto did not allow an ordinary command")
	}
	if decider.calls != 0 {
		t.Fatalf("full-auto asked %d times", decider.calls)
	}
}

type deciderContextKey struct{}

func TestTerminalDeciderInteractive(t *testing.T) {
	var out strings.Builder
	in := bufio.NewReader(strings.NewReader("y\n"))
	decider := NewTerminalDecider(in, &out)

	c := Confirmation{Action: "Run command", Detail: "make check"}
	if !decider.Confirm(context.Background(), c) {
		t.Fatal("expected TerminalDecider to return true on 'y'")
	}
	if !strings.Contains(out.String(), "make check") {
		t.Fatalf("expected output to contain detail: %s", out.String())
	}

	out.Reset()
	inNo := bufio.NewReader(strings.NewReader("no\n"))
	deciderNo := NewTerminalDecider(inNo, &out)
	if deciderNo.Confirm(context.Background(), c) {
		t.Fatal("expected TerminalDecider to return false on 'no'")
	}
}

func TestEngineFailsClosedWithoutDecider(t *testing.T) {
	ag := New(Options{Sess: enginetest.NewFakeSession("s_1", "model")})
	if firstOf(ag.confirm(context.Background(), Confirmation{Action: "write", Detail: "secret.txt"})) {
		t.Fatal("expected engine to fail closed when no Decider or In is configured")
	}
}

func TestSessionDeciderCachesApproval(t *testing.T) {
	underlying := &recordingDecider{allow: true}
	sessionDecider := NewSessionDecider(underlying)

	c := Confirmation{Action: "bash", Detail: "go test ./..."}

	// Manually set rule to allow_session
	sessionDecider.rules[c.Action+"::"+c.Detail] = protocol.PermissionDecisionAllowSession

	if !sessionDecider.Confirm(context.Background(), c) {
		t.Fatal("expected cached rule to allow action")
	}

	// Underlying decider should NOT have been consulted
	if underlying.calls != 0 {
		t.Fatalf("expected 0 calls to underlying decider, got %d", underlying.calls)
	}
}

func TestTerminalDeciderAlwaysReturnsAllowSession(t *testing.T) {
	var out strings.Builder
	in := bufio.NewReader(strings.NewReader("a\n"))
	decider := NewTerminalDecider(in, &out)

	c := Confirmation{Action: "write_file", Detail: "main.go"}
	decision := decider.Decide(context.Background(), c)
	if decision != protocol.PermissionDecisionAllowSession {
		t.Fatalf("decision = %q, want %q", decision, protocol.PermissionDecisionAllowSession)
	}
}

func TestPermissionEventsEmittedOnBus(t *testing.T) {
	sessID := xid.New(xid.Session)
	sess := enginetest.NewFakeSession(sessID, "model")

	b, err := bus.New(sessID, bus.Options{})
	if err != nil {
		t.Fatalf("bus.New: %v", err)
	}

	sub, err := b.Subscribe(0)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	decider := &recordingDecider{allow: true}
	ag := New(Options{
		Sess:    sess,
		Decider: decider,
		Bus:     b,
	})

	if !firstOf(ag.confirm(context.Background(), Confirmation{Action: "bash", Detail: "ls -la"})) {
		t.Fatal("confirm failed")
	}

	var events []protocol.Envelope
	for len(sub.Events()) > 0 {
		events = append(events, <-sub.Events())
	}

	if len(events) != 2 {
		t.Fatalf("received %d events, want 2 (requested, resolved)", len(events))
	}

	if events[0].Type != protocol.EventPermissionRequested {
		t.Fatalf("event[0].Type = %q, want %q", events[0].Type, protocol.EventPermissionRequested)
	}
	if events[1].Type != protocol.EventPermissionResolved {
		t.Fatalf("event[1].Type = %q, want %q", events[1].Type, protocol.EventPermissionResolved)
	}
}

// firstOf keeps the confirm-returns-a-decision change from rewriting every
// assertion that only cares whether the action was allowed.
func firstOf(allowed bool, _ protocol.PermissionDecision) bool { return allowed }
