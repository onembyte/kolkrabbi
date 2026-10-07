package cli

// From the round-6 vendor-recovery verifier (green on the tree it reviewed).

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
)

// R6-N1. Session A pauses at a plan limit the vendor closed (continuable).
// /new; session B works. A new process then reopens A from A's own file and
// /resume continues A's request on A's conversation, and B's conversation
// sees nothing of it. Then B's restart works on B's conversation.
func TestRound6OldSessionResumesOnItsOwnConversationAfterNewAndRestart(t *testing.T) {
	argv, requests, side := round5Setup(t)
	sessA := enginetest.NewFakeSession("s_r6_A", "claude-opus")
	sessA.SetProviderStateName("H-A")
	var first *engine.Agent
	opts := engine.Options{Backend: round5Decorated(t, sessA, &first), Model: "claude-opus", Mode: engine.ModeCode, Effort: engine.EffortHigh,
		Permission: engine.PermissionFullAuto, Sess: sessA, Root: t.TempDir(), Out: io.Discard}
	first = engine.New(opts)
	var paused *engine.PausedError
	if err := first.RunTurn(context.Background(), "session A LIMIT work"); !errors.As(err, &paused) {
		t.Fatalf("setup: A not paused: %v", err)
	}
	sessB := enginetest.NewFakeSession("s_r6_B", "claude-opus")
	first.ReplaceSession(sessB, nil)
	if err := first.RunTurn(context.Background(), "session B work"); err != nil {
		t.Fatalf("setup B: %v", err)
	}
	_ = first.Close()
	t.Logf("files: A=%q B=%q", sessA.ProviderStateName(), sessB.ProviderStateName())
	if sessA.ProviderStateName() != "H-A" || sessB.ProviderStateName() == "" || sessB.ProviderStateName() == "H-A" {
		t.Fatalf("session files wrong: A=%q B=%q", sessA.ProviderStateName(), sessB.ProviderStateName())
	}
	// Restart on A.
	var second *engine.Agent
	optsA := opts
	optsA.Sess, optsA.Backend = sessA, round5Decorated(t, sessA, &second)
	second = engine.New(optsA)
	pending, ok := second.Resume()
	if !ok {
		t.Fatal("A: nothing to resume")
	}
	errA := second.RunTurn(context.Background(), pending)
	_ = second.Close()
	lines := round4Lines(side)
	t.Logf("A resume err=%v side=%q spawns=%d requests=%d", errA, lines, len(round4Lines(argv)), len(round4Lines(requests)))
	if errA != nil {
		t.Fatalf("A's continuable work was refused after /new + restart: %v", errA)
	}
	last := lines[len(lines)-1]
	if last != "continued in H-A" {
		t.Fatalf("A's continuation ran as %q; want it continued in H-A", last)
	}
	for _, l := range lines {
		if strings.Contains(l, sessB.ProviderStateName()) && strings.Contains(l, "LIMIT") {
			t.Fatalf("A's work reached B's conversation: %q", l)
		}
	}
	if sessA.ProviderStateName() != "H-A" {
		t.Fatalf("A's file now %q", sessA.ProviderStateName())
	}
}
