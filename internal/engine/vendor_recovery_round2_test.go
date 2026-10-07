package engine

// Found by the round-2 vendor-recovery verifier: each was red on the tree
// it reviewed and pins the contract v3 rule it names.

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

// probeRecorder is any backend that answers and records what it was sent.
type probeRecorder struct {
	mu   sync.Mutex
	sent [][]provider.Message
}

func (r *probeRecorder) StreamChat(_ context.Context, _ string, msgs []provider.Message, _ []provider.Tool, _ func(string)) (provider.Message, provider.Meta, error) {
	r.mu.Lock()
	r.sent = append(r.sent, append([]provider.Message(nil), msgs...))
	r.mu.Unlock()
	for _, m := range msgs {
		if strings.Contains(m.Content, "Decompose the request") {
			return provider.Message{Role: "assistant", Content: `[{"title":"first","kind":"explain"},{"title":"second","kind":"explain"}]`}, provider.Meta{}, nil
		}
	}
	return provider.Message{Role: "assistant", Content: "answered"}, provider.Meta{}, nil
}

func (r *probeRecorder) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sent)
}

func (r *probeRecorder) sawContinuation(marker string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, msgs := range r.sent {
		for _, m := range msgs {
			if strings.Contains(m.Content, marker) {
				return true
			}
		}
	}
	return false
}

// probeVendorMain is a resumable vendor main that always answers.
type probeVendorMain struct {
	probeRecorder
	handle string
}

func (m *probeVendorMain) ProviderHandle() string        { return m.handle }
func (m *probeVendorMain) ProviderHandleConfirmed() bool { return true }
func (m *probeVendorMain) TurnNeverStarted() bool        { return false }
func (m *probeVendorMain) TurnClosed() bool              { return true }
func (m *probeVendorMain) ResumesConversation() bool     { return true }

// P2. A child that must continue its saved vendor conversation is reopened,
// and the subagent port answers nil (the engine then shares the session's
// own provider). The continuation is sent to a conversation that never saw
// the task's work.
func TestRound2ChildContinuationNeverMovesToTheSessionBackend(t *testing.T) {
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_probe_child_nil", "parent")}
	root := t.TempDir()
	session := &probeRecorder{}
	opts := Options{Backend: session, Model: "parent", Mode: ModeAgent, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: root, Out: io.Discard, MaxConcurrentTasks: 1}
	opts.SubagentBackend = func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
		return resumableVendorChild{vendorChild{handle: "accepted-h1", confirmed: true, closed: true, err: vendorLimit, events: finishedTool}}, nil
	}
	first := New(opts)
	var paused *PausedError
	if err := first.RunTurn(context.Background(), "two things"); !errors.As(err, &paused) {
		t.Fatalf("not paused: %v", err)
	}
	_ = first.Close()
	if run := sess.RunState(); run == nil || run.Tasks[0].ProviderState != "accepted-h1" {
		t.Fatalf("setup: task 1 not saved on its conversation: %+v", run)
	}
	opts.SubagentBackend = func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
		return nil, nil // "not a vendor rung": share the session's provider
	}
	second := New(opts)
	defer second.Close()
	pending, ok := second.Resume()
	if !ok {
		t.Fatal("nothing to resume")
	}
	err := second.RunTurn(context.Background(), pending)
	if session.sawContinuation("Continue the unfinished assigned task") {
		t.Fatalf("IDENTITY: task 1's continuation was sent to the session's own provider, not its saved conversation accepted-h1 (resume err=%v)", err)
	}
}

// P2b. The reopened child drives a different conversation from the saved
// one. The engine checks this for the main session; does it for a child?
func TestRound2ChildContinuationNeverMovesToAnotherHandle(t *testing.T) {
	var other probeRecorder
	var mu sync.Mutex
	reopen := func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
		return resumableVendorChild{vendorChild{handle: "other-h", confirmed: true, closed: true,
			record: func(msgs []provider.Message) {
				mu.Lock()
				defer mu.Unlock()
				other.sent = append(other.sent, msgs)
			}}}, nil
	}
	first := resumableVendorChild{vendorChild{handle: "accepted-h1", confirmed: true, closed: true, err: vendorLimit, events: finishedTool}}
	_, _, err := vendorRecoveryRun(t, first, reopen)
	mu.Lock()
	defer mu.Unlock()
	if other.sawContinuation("Continue the unfinished assigned task") {
		t.Fatalf("IDENTITY: task 1's continuation went to conversation other-h, not its saved accepted-h1 (resume err=%v)", err)
	}
}

// P4. A session saved by a build with no run journal (v1.3.4) was paused on
// a vendor plan limit: only the pending input and the session's vendor
// handle exist. v3: "a journal saved before these facts existed" is
// refused, its work retained. The pending turn is instead sent again.
func TestRound2LegacyPauseOnlyVendorSessionIsRefused(t *testing.T) {
	sess := enginetest.NewFakeSession("s_probe_legacy_pause", "mock/model")
	sess.SetProviderStateName("old-h")
	sess.SetPaused(&continuity.Pause{Kind: string(provider.LimitSubscriptionAllowance), Scope: string(provider.ScopeAccount),
		Model: "mock/model", Since: time.Now().Add(-time.Hour), ResetAt: time.Now().Add(-time.Minute), PendingTurn: "deploy the service"})
	vendor := &probeVendorMain{handle: "old-h"}
	a := New(Options{Backend: vendor, Model: "mock/model", Mode: ModeCode, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard})
	defer a.Close()
	pending, ok := a.Resume()
	if !ok {
		t.Fatal("the legacy pause was not offered")
	}
	err := a.RunTurn(context.Background(), pending)
	if n := vendor.calls(); n > 0 {
		t.Fatalf("LEGACY: a pre-journal vendor pause re-sent its pending turn %q to conversation old-h (%d calls, err=%v); nothing proves the vendor's turn finished",
			pending, n, err)
	}
}
