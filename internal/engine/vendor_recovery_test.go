package engine

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

// vendorChild is a vendor-owned child: it names a conversation and says what
// its adapter proves about the turn it ran: whether the vendor confirmed the
// conversation, whether the prompt provably never arrived, whether the vendor
// closed the turn. Its tools are reported only through progress events. It
// cannot say it resumes a conversation; resumableVendorChild can.
type vendorChild struct {
	handle       string
	confirmed    bool
	neverStarted bool
	closed       bool
	events       []provider.ProgressEvent
	err          error
	record       func(msgs []provider.Message)
}

func (c vendorChild) StreamChat(ctx context.Context, model string, msgs []provider.Message, tools []provider.Tool, onToken func(string)) (provider.Message, provider.Meta, error) {
	return c.StreamChatObserved(ctx, model, msgs, tools, onToken, nil)
}

func (c vendorChild) StreamChatObserved(_ context.Context, _ string, msgs []provider.Message, _ []provider.Tool, _ func(string), observe func(provider.ProgressEvent)) (provider.Message, provider.Meta, error) {
	if c.record != nil {
		c.record(msgs)
	}
	for _, event := range c.events {
		if observe != nil {
			observe(event)
		}
	}
	if c.err != nil {
		return provider.Message{}, provider.Meta{}, c.err
	}
	return provider.Message{Role: "assistant", Content: "done"}, provider.Meta{}, nil
}

func (c vendorChild) ProviderHandle() string        { return c.handle }
func (c vendorChild) ProviderHandleConfirmed() bool { return c.confirmed }
func (c vendorChild) TurnNeverStarted() bool        { return c.neverStarted }
func (c vendorChild) TurnClosed() bool              { return c.closed }

type resumableVendorChild struct{ vendorChild }

func (resumableVendorChild) ResumesConversation() bool { return true }

type resumedOpen struct {
	model  string
	handle string
	sent   []provider.Message
}

// vendorRecoveryRun stops a two-task plan at its first vendor task on a plan
// limit, then resumes it from the saved journal on a new agent, as a restarted
// process would. reopen opens each task on the resumed run; nil answers as a
// resumable vendor would. It returns what was opened and sent, and the
// resume's error.
func vendorRecoveryRun(t *testing.T, first ChatBackend, reopen SubagentBackend) (*reviewSession, []resumedOpen, error) {
	t.Helper()
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_vendor_recovery", "parent")}
	root := t.TempDir()
	opts := Options{
		Backend: reviewPlanner{titles: []string{"first", "second"}}, Model: "parent", Mode: ModeAgent, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: root, Out: io.Discard, MaxConcurrentTasks: 1,
	}
	opts.SubagentBackend = func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
		return first, nil
	}
	a := New(opts)
	var paused *PausedError
	if err := a.RunTurn(context.Background(), "two things"); !errors.As(err, &paused) {
		t.Fatalf("not paused: %v", err)
	}
	_ = a.Close()

	var mu sync.Mutex
	var opens []resumedOpen
	opts.SubagentBackend = func(ctx context.Context, model, mode, effort string, caps SubagentCapabilities) (ChatBackend, error) {
		mu.Lock()
		opens = append(opens, resumedOpen{model: model, handle: caps.ProviderState})
		index := len(opens) - 1
		mu.Unlock()
		if reopen != nil {
			return reopen(ctx, model, mode, effort, caps)
		}
		return resumableVendorChild{vendorChild{handle: caps.ProviderState, confirmed: true, closed: true,
			record: func(msgs []provider.Message) {
				mu.Lock()
				opens[index].sent = append([]provider.Message(nil), msgs...)
				mu.Unlock()
			}}}, nil
	}
	b := New(opts)
	defer b.Close()
	pending, ok := b.Resume()
	if !ok {
		t.Fatal("nothing to resume after the pause")
	}
	resumeErr := b.RunTurn(context.Background(), pending)
	mu.Lock()
	defer mu.Unlock()
	return sess, append([]resumedOpen(nil), opens...), resumeErr
}

func lastSent(open resumedOpen) string {
	if len(open.sent) == 0 {
		return ""
	}
	return open.sent[len(open.sent)-1].Content
}

var vendorLimit = provider.Limit{Kind: provider.LimitSubscriptionAllowance, Scope: provider.ScopeAccount,
	Model: "child", ResetAt: time.Now().Add(time.Hour)}

var finishedTool = []provider.ProgressEvent{
	{Kind: provider.ProgressToolStarted, ID: "call_a", Name: "Bash"},
	{Kind: provider.ProgressToolFinished, ID: "call_a", Name: "Bash"},
}

// A turn whose prompt provably never reached the vendor holds nothing to
// continue and nothing that could be repeated: the task starts over on a new
// conversation from its own briefing, and the minted handle is not reopened.
func TestANeverDeliveredVendorTaskStartsFresh(t *testing.T) {
	_, opens, err := vendorRecoveryRun(t, vendorChild{handle: "minted-never-delivered", neverStarted: true, err: vendorLimit}, nil)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if len(opens) == 0 {
		t.Fatal("the first task never ran again")
	}
	if opens[0].handle != "" {
		t.Errorf("the never-delivered handle %q was reopened", opens[0].handle)
	}
	if last := lastSent(opens[0]); strings.Contains(last, "Continue") || !strings.Contains(last, "Your task: first") {
		t.Errorf("the fresh task was sent %q; want its own briefing", last)
	}
}

// A turn the vendor closed itself, a plan limit's result frame for one, with
// nothing it started left unfinished, continues on its own confirmed
// conversation with a new message. This is the automatic recovery after a
// plan limit.
func TestAClosedVendorTurnContinuesOnItsOwnConversation(t *testing.T) {
	first := resumableVendorChild{vendorChild{handle: "accepted-h1", confirmed: true, closed: true, err: vendorLimit, events: finishedTool}}
	_, opens, err := vendorRecoveryRun(t, first, nil)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if len(opens) == 0 || opens[0].handle != "accepted-h1" {
		t.Fatalf("the closed turn was reopened on %+v; want its own conversation", opens)
	}
	if last := lastSent(opens[0]); !strings.Contains(last, "Continue the unfinished assigned task") {
		t.Errorf("the continuation was %q", last)
	}
}

// Found by Codex's review and the vendor-recovery verifier. Nothing proves a
// continuation or a fresh start safe here, so the task is refused and its
// work retained: asking the model to inspect is not reconciliation.
func TestAVendorTaskNothingProvesSafeIsRefused(t *testing.T) {
	unfinished := append(append([]provider.ProgressEvent(nil), finishedTool...),
		provider.ProgressEvent{Kind: provider.ProgressToolStarted, ID: "call_b", Name: "Edit"})
	for _, c := range []struct {
		name  string
		first ChatBackend
		why   string
	}{
		{"a tool never reported finishing", resumableVendorChild{vendorChild{handle: "h", confirmed: true, closed: true, err: vendorLimit, events: unfinished}}, "never reported finishing"},
		{"the vendor never closed its turn", resumableVendorChild{vendorChild{handle: "h", confirmed: true, err: vendorLimit, events: finishedTool}}, "never closed its turn"},
		{"no continuation capability", vendorChild{handle: "h", confirmed: true, closed: true, err: vendorLimit}, "cannot continue"},
		{"an unconfirmed conversation", resumableVendorChild{vendorChild{handle: "minted", closed: true, err: vendorLimit, events: finishedTool}}, "cannot be reached"},
		{"no proof the prompt never arrived", resumableVendorChild{vendorChild{err: vendorLimit}}, "never closed its turn"},
	} {
		t.Run(c.name, func(t *testing.T) {
			sess, opens, err := vendorRecoveryRun(t, c.first, nil)
			if err == nil || !strings.Contains(err.Error(), "task 1") || !strings.Contains(err.Error(), c.why) ||
				!strings.Contains(err.Error(), "repeat what the vendor already did") {
				t.Fatalf("resume = %v; want task 1 refused because %s", err, c.why)
			}
			if len(opens) != 0 {
				t.Fatalf("a refused resume still opened %d tasks", len(opens))
			}
			if run := sess.RunState(); run == nil || run.Phase == "stopped" || run.Phase == "done" ||
				!run.Tasks[0].ProviderOwned || run.Tasks[0].Status != "" {
				t.Fatalf("the refused run was not kept intact: %+v", run)
			}
		})
	}
}

// A settled task is an outcome, not a conversation to recover: whatever its
// vendor state, it is neither refused nor rerun.
func TestASettledVendorTaskIsLeftAlone(t *testing.T) {
	root := t.TempDir()
	sess := enginetest.NewFakeSession("s_settled_vendor", "parent")
	sess.SetRunState(&continuity.Run{Version: 1, ID: "t_settled", Input: "two things", Prompt: "two things", Root: root,
		Mode: ModeAgent, Model: "parent", Effort: EffortMedium, Phase: "tasks", Recovery: "error",
		Main: continuity.Task{State: continuity.TaskWaiting},
		Tasks: []continuity.Task{
			{Title: "first", Kind: string(KindExplain), Model: "parent", Effort: EffortMedium, Status: "failed", State: continuity.TaskSettled,
				Result: "it failed", ProviderOwned: true, ProviderState: "h", ProviderInFlight: true},
			{Title: "second", Kind: string(KindExplain), Model: "parent", Effort: EffortMedium, State: continuity.TaskQueued},
		}})
	var mu sync.Mutex
	opened := 0
	a := New(Options{Backend: reviewPlanner{titles: []string{"first", "second"}}, Model: "parent", Mode: ModeAgent, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: root, Out: io.Discard, MaxConcurrentTasks: 1,
		SubagentBackend: func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
			mu.Lock()
			opened++
			mu.Unlock()
			return childCaptureBackend{text: "second done"}, nil
		}})
	defer a.Close()
	pending, ok := a.Resume()
	if !ok {
		t.Fatal("nothing to resume")
	}
	if err := a.RunTurn(context.Background(), pending); err != nil {
		t.Fatalf("a settled vendor task blocked the resume: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if opened != 1 {
		t.Fatalf("opened %d tasks, want only the unsettled second", opened)
	}
}

// editPlanner plans two writing tasks, then synthesizes.
type editPlanner struct{}

func (editPlanner) StreamChat(_ context.Context, _ string, messages []provider.Message, _ []provider.Tool, _ func(string)) (provider.Message, provider.Meta, error) {
	for _, m := range messages {
		if strings.Contains(m.Content, "Decompose the request") {
			return provider.Message{Role: "assistant", Content: `[{"title":"first","kind":"edit"},{"title":"second","kind":"edit"}]`}, provider.Meta{}, nil
		}
	}
	return provider.Message{Role: "assistant", Content: "all done"}, provider.Meta{}, nil
}

// Found by the vendor-recovery verifier. A task that must continue its saved
// conversation never falls back to another model when its own will not start:
// that would be a new conversation told to continue work it never saw. The
// task is held like a paused one: its checkpoint stays open, nothing is
// settled, and the run stops at one recovery boundary with the handle intact.
func TestAContinuationWhoseModelCannotStartIsHeld(t *testing.T) {
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_held_continuation", "parent")}
	ckpt := &enginetest.FakeCheckpointer{}
	root := t.TempDir()
	opts := Options{Backend: editPlanner{}, Model: "parent", Mode: ModeAgent, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Ckpt: ckpt, Root: root, Out: io.Discard, MaxConcurrentTasks: 1,
		SubagentBackend: func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
			return resumableVendorChild{vendorChild{handle: "accepted-h1", confirmed: true, closed: true, err: vendorLimit, events: finishedTool}}, nil
		}}
	first := New(opts)
	var paused *PausedError
	if err := first.RunTurn(context.Background(), "two edits"); !errors.As(err, &paused) {
		t.Fatalf("not paused: %v", err)
	}
	_ = first.Close()
	// The task's own model differs from the ceiling, so a fallback is possible.
	run := sess.RunState()
	run.Tasks[0].Model = "child-model"
	sess.SetRunState(run)
	ended := len(ckpt.Ended)

	var opened []string
	opts.SubagentBackend = func(_ context.Context, model, _, _ string, caps SubagentCapabilities) (ChatBackend, error) {
		opened = append(opened, model+"@"+caps.ProviderState)
		return nil, errors.New("the connector is signed out")
	}
	second := New(opts)
	defer second.Close()
	pending, ok := second.Resume()
	if !ok {
		t.Fatal("nothing to resume")
	}
	err := second.RunTurn(context.Background(), pending)
	if err == nil || !strings.Contains(err.Error(), "cannot start now") {
		t.Fatalf("resume = %v; want the run held because the task's model cannot start", err)
	}
	if len(opened) != 1 || opened[0] != "child-model@accepted-h1" {
		t.Fatalf("opened %q; want one attempt on the task's own model and conversation, no fallback", opened)
	}
	if len(ckpt.Ended) != ended {
		t.Fatalf("the held task's checkpoint was ended: %v", ckpt.Ended)
	}
	if reasons, _ := sess.written(); len(reasons) != 2 || reasons[0] != "pause" || reasons[1] != "error" {
		t.Fatalf("recovery writes %q; want the pause, then one write for the held stop", reasons)
	}
	held := sess.RunState()
	if held == nil || held.Phase == "done" || held.Phase == "stopped" || held.Recovery == "" ||
		held.Tasks[0].Status != "" || held.Tasks[0].ProviderState != "accepted-h1" {
		t.Fatalf("the held run = %+v; want it retained at a recovery boundary with the task's handle", held)
	}
}

// The adapter's proofs are recorded where the handle is, so a restart decides
// from the journal before any provider is opened.
func TestTheJournalRecordsTheAdaptersProofs(t *testing.T) {
	agent := New(Options{Backend: resumableVendorChild{vendorChild{handle: "main-h", confirmed: true, closed: true}}, Model: "parent", Mode: ModeAgent,
		Sess: enginetest.NewFakeSession("s_proofs", "parent"), Root: t.TempDir(), Out: io.Discard})
	agent.beginExecution("request", "request")
	agent.setExecutionPlan([]Task{{Title: "a", Model: "child", Effort: EffortMedium}, {Title: "b", Model: "child", Effort: EffortMedium}})
	msgs := []provider.Message{{Role: "user", Content: "work"}}
	agent.saveChildConversation(0, msgs, 1, doomLoop{}, resumableVendorChild{vendorChild{handle: "h0", confirmed: true, neverStarted: true}})
	agent.saveChildConversation(1, msgs, 1, doomLoop{}, vendorChild{handle: "h1", confirmed: true, closed: true})
	agent.captureMainProviderState(agent.sessionBackend())
	run := agent.executionSnapshot()
	for name, c := range map[string]struct {
		task                            continuity.Task
		resumable, neverStarted, closed bool
	}{
		"task 0": {run.Tasks[0], true, true, false},
		"task 1": {run.Tasks[1], false, false, true},
		"main":   {run.Main, true, false, true},
	} {
		if c.task.ProviderResumable != c.resumable || c.task.ProviderNeverStarted != c.neverStarted || c.task.ProviderTurnClosed != c.closed {
			t.Errorf("%s: resumable %v, never started %v, closed %v; want %v, %v, %v", name,
				c.task.ProviderResumable, c.task.ProviderNeverStarted, c.task.ProviderTurnClosed, c.resumable, c.neverStarted, c.closed)
		}
	}
}

// Found by Codex's independent review. A journal saved before these facts
// were recorded proves nothing about its vendor turns, so its vendor work is
// refused, not continued on a bare handle.
func TestALegacyVendorJournalIsRefused(t *testing.T) {
	root := t.TempDir()
	sess := enginetest.NewFakeSession("s_legacy_vendor", "parent")
	briefing := []provider.Message{{Role: "system", Content: "briefing"}, {Role: "user", Content: "Your task: first"}}
	sess.SetRunState(&continuity.Run{Version: 1, ID: "t_legacy", Input: "two things", Prompt: "two things", Root: root,
		Mode: ModeAgent, Model: "parent", Effort: EffortMedium, Phase: "tasks",
		Tasks: []continuity.Task{
			{Title: "first", Kind: string(KindExplain), Model: "parent", Effort: EffortMedium, ProviderState: "old-h", Messages: briefing},
			{Title: "second", Kind: string(KindExplain), Model: "parent", Effort: EffortMedium},
		},
		LastPause: &continuity.Pause{Kind: string(provider.LimitSubscriptionAllowance), Scope: string(provider.ScopeAccount),
			Model: "parent", Since: time.Now().Add(-time.Hour), ResetAt: time.Now().Add(-time.Minute), PendingTurn: "two things"}})
	opened := 0
	a := New(Options{Backend: reviewPlanner{titles: []string{"first", "second"}}, Model: "parent", Mode: ModeAgent, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: root, Out: io.Discard, MaxConcurrentTasks: 1,
		SubagentBackend: func(_ context.Context, _, _, _ string, caps SubagentCapabilities) (ChatBackend, error) {
			opened++
			return resumableVendorChild{vendorChild{handle: caps.ProviderState, confirmed: true, closed: true}}, nil
		}})
	defer a.Close()
	pending, ok := a.Resume()
	if !ok {
		t.Fatal("the legacy pause was not offered")
	}
	err := a.RunTurn(context.Background(), pending)
	if err == nil || !strings.Contains(err.Error(), "task 1") || !strings.Contains(err.Error(), "nothing proves its turn finished") || opened != 0 {
		t.Fatalf("legacy resume = %v after %d opens; want a refusal before any provider", err, opened)
	}
}

// scriptedVendorMain is a vendor-owned main session whose first turn stops
// and whose later turns answer, remembering what they were sent.
type scriptedVendorMain struct {
	mu           sync.Mutex
	handle       string
	confirmed    bool
	neverStarted bool
	closed       bool
	events       []provider.ProgressEvent
	stop         error
	calls        int
	sent         [][]provider.Message
}

func (m *scriptedVendorMain) StreamChat(ctx context.Context, model string, msgs []provider.Message, tools []provider.Tool, onToken func(string)) (provider.Message, provider.Meta, error) {
	return m.StreamChatObserved(ctx, model, msgs, tools, onToken, nil)
}

func (m *scriptedVendorMain) StreamChatObserved(_ context.Context, _ string, msgs []provider.Message, _ []provider.Tool, _ func(string), observe func(provider.ProgressEvent)) (provider.Message, provider.Meta, error) {
	m.mu.Lock()
	m.calls++
	first := m.calls == 1
	m.sent = append(m.sent, append([]provider.Message(nil), msgs...))
	m.mu.Unlock()
	if first {
		for _, event := range m.events {
			if observe != nil {
				observe(event)
			}
		}
		return provider.Message{}, provider.Meta{}, m.stop
	}
	return provider.Message{Role: "assistant", Content: "finished"}, provider.Meta{}, nil
}

func (m *scriptedVendorMain) ProviderHandle() string        { return m.handle }
func (m *scriptedVendorMain) ProviderHandleConfirmed() bool { return m.confirmed }
func (m *scriptedVendorMain) TurnNeverStarted() bool        { return m.neverStarted }
func (m *scriptedVendorMain) TurnClosed() bool              { return m.closed }

type resumableVendorMain struct{ *scriptedVendorMain }

func (resumableVendorMain) ResumesConversation() bool { return true }

// A vendor-owned main session stopped by an ordinary error recovers by the
// same rules after a restart.
func TestAVendorMainSessionRecoversFromAnErrorByTheSameRules(t *testing.T) {
	unfinished := []provider.ProgressEvent{{Kind: provider.ProgressToolStarted, ID: "call_x", Name: "Bash"}}
	exited := errors.New("the vendor process exited")
	for _, c := range []struct {
		name      string
		resumable bool
		main      *scriptedVendorMain
		refused   string
		continued bool
	}{
		{"never delivered", true, &scriptedVendorMain{handle: "minted", neverStarted: true, stop: exited}, "", false},
		{"closed and resumable", true, &scriptedVendorMain{handle: "main-h", confirmed: true, closed: true, stop: exited}, "", true},
		{"a tool never finished", true, &scriptedVendorMain{handle: "main-h", confirmed: true, closed: true, events: unfinished, stop: exited}, "never reported finishing", false},
		{"never closed", true, &scriptedVendorMain{handle: "main-h", confirmed: true, stop: exited}, "never closed its turn", false},
		{"not resumable", false, &scriptedVendorMain{handle: "main-h", confirmed: true, closed: true, stop: exited}, "cannot continue", false},
		{"no proof it never started", true, &scriptedVendorMain{stop: exited}, "never closed its turn", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_vendor_main", "mock/model")}
			var backend ChatBackend = c.main
			if c.resumable {
				backend = resumableVendorMain{c.main}
			}
			opts := Options{Backend: backend, Model: "mock/model", Mode: ModeCode, Effort: EffortMedium,
				Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard}
			a := New(opts)
			if err := a.RunTurn(context.Background(), "do the thing"); err == nil {
				t.Fatal("the vendor error did not stop the turn")
			}
			_ = a.Close()
			b := New(opts)
			defer b.Close()
			pending, ok := b.Resume()
			if !ok {
				t.Fatal("nothing to resume after the error")
			}
			err := b.RunTurn(context.Background(), pending)
			c.main.mu.Lock()
			defer c.main.mu.Unlock()
			if c.refused != "" {
				if err == nil || !strings.Contains(err.Error(), c.refused) || c.main.calls != 1 {
					t.Fatalf("resume = %v after %d calls; want a refusal (%s) before any call", err, c.main.calls, c.refused)
				}
				return
			}
			if err != nil || c.main.calls != 2 {
				t.Fatalf("resume = %v after %d calls; want one more call", err, c.main.calls)
			}
			last := c.main.sent[1][len(c.main.sent[1])-1].Content
			if continued := strings.Contains(last, "Continue"); continued != c.continued {
				t.Fatalf("the resumed call ended with %q; continuation %v, want %v", last, continued, c.continued)
			}
		})
	}
}

// Found by the vendor-recovery verifier and Codex's review. A saved vendor
// conversation belongs to its vendor and to that conversation alone, at a
// pause as at an error: a session that now answers natively, or drives a
// different conversation, is refused before any call.
func TestASavedVendorRequestStaysOnItsOwnConversation(t *testing.T) {
	for _, boundary := range []string{"error", "pause"} {
		for _, c := range []struct {
			name  string
			later func() (ChatBackend, func() int)
			why   string
		}{
			{"now native", func() (ChatBackend, func() int) {
				native := &countingBackend{}
				return native, func() int { return int(native.calls.Load()) }
			}, "vendor this session no longer uses"},
			{"another conversation", func() (ChatBackend, func() int) {
				other := &scriptedVendorMain{handle: "other-h", confirmed: true, closed: true}
				return resumableVendorMain{other}, func() int { other.mu.Lock(); defer other.mu.Unlock(); return other.calls }
			}, "different vendor conversation"},
		} {
			t.Run(boundary+"/"+c.name, func(t *testing.T) {
				sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_vendor_owner", "mock/model")}
				root := t.TempDir()
				stop := error(errors.New("the vendor process exited"))
				if boundary == "pause" {
					stop = provider.Limit{Kind: provider.LimitSubscriptionAllowance, Scope: provider.ScopeAccount,
						Model: "mock/model", ResetAt: time.Now().Add(time.Hour)}
				}
				main := &scriptedVendorMain{handle: "main-h", confirmed: true, closed: true, stop: stop}
				first := New(Options{Backend: resumableVendorMain{main}, Model: "mock/model", Mode: ModeCode, Effort: EffortMedium,
					Permission: PermissionFullAuto, Sess: sess, Root: root, Out: io.Discard})
				if err := first.RunTurn(context.Background(), "do the thing"); err == nil {
					t.Fatal("the vendor stop did not stop the turn")
				}
				_ = first.Close()
				backend, calls := c.later()
				second := New(Options{Backend: backend, Model: "mock/model", Mode: ModeCode, Effort: EffortMedium,
					Permission: PermissionFullAuto, Sess: sess, Root: root, Out: io.Discard})
				defer second.Close()
				pending, ok := second.Resume()
				if !ok {
					t.Fatal("nothing to resume")
				}
				err := second.RunTurn(context.Background(), pending)
				if err == nil || !strings.Contains(err.Error(), c.why) || calls() != 0 {
					t.Fatalf("resume = %v after %d calls; want a refusal (%s) before any call", err, calls(), c.why)
				}
			})
		}
	}
}

// The rules on hand-built journals: a turn the vendor committed needs no
// proof to go on, whatever the adapter can do; a journal claiming the prompt
// never arrived while reporting vendor tools contradicts itself and proves
// nothing, so it is refused.
func TestTheVendorRecoveryRulesOnSavedJournals(t *testing.T) {
	committed := continuity.Task{State: continuity.TaskWaiting, ProviderOwned: true, ProviderState: "h", ProviderConfirmed: true}
	contradictory := continuity.Task{State: continuity.TaskWaiting, ProviderOwned: true, ProviderState: "h", ProviderInFlight: true,
		ProviderNeverStarted: true, VendorTools: &continuity.VendorToolBoundary{LastFinishedID: "call_a"}}
	for _, c := range []struct {
		name    string
		main    continuity.Task
		refused bool
	}{{"a committed turn", committed, false}, {"a contradictory journal", contradictory, true}} {
		t.Run(c.name, func(t *testing.T) {
			run := &continuity.Run{Version: 1, Main: c.main}
			err := recoverVendorWork(run)
			if (err != nil) != c.refused {
				t.Fatalf("recoverVendorWork = %v, refused want %v", err, c.refused)
			}
			if !c.refused && run.Main.ProviderState != "h" {
				t.Fatalf("a committed conversation lost its handle: %+v", run.Main)
			}
		})
	}
}

// Found as surviving mutants by the round-2 verifier. A paused vendor
// conversation cannot follow a changed selection: re-resolving its route
// would hand the conversation to another provider. Both the main session's
// and a child's are refused before any call.
func TestAPausedVendorConversationRefusesAChangedSelection(t *testing.T) {
	t.Run("a child", func(t *testing.T) {
		sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_changed_child", "parent")}
		root := t.TempDir()
		opts := Options{Backend: reviewPlanner{titles: []string{"first", "second"}}, Model: "parent", Mode: ModeAgent, Effort: EffortMedium,
			Permission: PermissionFullAuto, Sess: sess, Root: root, Out: io.Discard, MaxConcurrentTasks: 1,
			SubagentBackend: func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
				return resumableVendorChild{vendorChild{handle: "accepted-h1", confirmed: true, closed: true, err: vendorLimit, events: finishedTool}}, nil
			}}
		first := New(opts)
		var paused *PausedError
		if err := first.RunTurn(context.Background(), "two things"); !errors.As(err, &paused) {
			t.Fatalf("not paused: %v", err)
		}
		_ = first.Close()
		opened := 0
		opts.Effort = EffortHigh
		opts.SubagentBackend = func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
			opened++
			return resumableVendorChild{vendorChild{handle: "accepted-h1", confirmed: true, closed: true}}, nil
		}
		second := New(opts)
		defer second.Close()
		pending, _ := second.Resume()
		err := second.RunTurn(context.Background(), pending)
		if err == nil || !strings.Contains(err.Error(), "unfinished provider conversation needs") || opened != 0 {
			t.Fatalf("resume after a changed effort = %v with %d opens; want a refusal before any provider", err, opened)
		}
	})
	t.Run("the main session", func(t *testing.T) {
		sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_changed_main", "mock/model")}
		root := t.TempDir()
		main := &scriptedVendorMain{handle: "main-h", confirmed: true, closed: true,
			stop: provider.Limit{Kind: provider.LimitSubscriptionAllowance, Scope: provider.ScopeAccount, Model: "mock/model", ResetAt: time.Now().Add(time.Hour)}}
		opts := Options{Backend: resumableVendorMain{main}, Model: "mock/model", Mode: ModeCode, Effort: EffortMedium,
			Permission: PermissionFullAuto, Sess: sess, Root: root, Out: io.Discard}
		first := New(opts)
		var paused *PausedError
		if err := first.RunTurn(context.Background(), "do the thing"); !errors.As(err, &paused) {
			t.Fatalf("not paused: %v", err)
		}
		_ = first.Close()
		opts.Effort = EffortHigh
		second := New(opts)
		defer second.Close()
		pending, _ := second.Resume()
		err := second.RunTurn(context.Background(), pending)
		main.mu.Lock()
		defer main.mu.Unlock()
		if err == nil || !strings.Contains(err.Error(), "unfinished provider conversation needs") || main.calls != 1 {
			t.Fatalf("resume after a changed effort = %v after %d calls; want a refusal before any call", err, main.calls)
		}
	})
}

type attemptScript struct {
	neverStarted, closed bool
	stream               string
	reply                string
	err                  error
}

// scriptedAttemptsMain is a vendor-owned main session whose provider calls
// follow a script: each says whether that attempt reached the vendor, whether
// the vendor closed it, and how it ended.
type scriptedAttemptsMain struct {
	mu       sync.Mutex
	attempts []attemptScript
	calls    int
	last     attemptScript
	sent     [][]provider.Message
}

func (m *scriptedAttemptsMain) StreamChat(_ context.Context, _ string, msgs []provider.Message, _ []provider.Tool, onToken func(string)) (provider.Message, provider.Meta, error) {
	m.mu.Lock()
	m.last = m.attempts[min(m.calls, len(m.attempts)-1)]
	m.calls++
	m.sent = append(m.sent, append([]provider.Message(nil), msgs...))
	attempt := m.last
	m.mu.Unlock()
	if attempt.stream != "" && onToken != nil {
		onToken(attempt.stream)
	}
	if attempt.err != nil {
		return provider.Message{}, provider.Meta{}, attempt.err
	}
	reply := attempt.reply
	if reply == "" {
		reply = "finished"
	}
	return provider.Message{Role: "assistant", Content: reply}, provider.Meta{}, nil
}

func (m *scriptedAttemptsMain) ProviderHandle() string        { return "main-h" }
func (m *scriptedAttemptsMain) ProviderHandleConfirmed() bool { return true }
func (m *scriptedAttemptsMain) ResumesConversation() bool     { return true }
func (m *scriptedAttemptsMain) TurnNeverStarted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.last.neverStarted
}
func (m *scriptedAttemptsMain) TurnClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.last.closed
}

// Found by the round-2 verifier. An attempt that never reached the vendor
// changes nothing the journal knows: after a vendor-closed turn, a
// continuation whose process could not start leaves the task continuable,
// neither "never started" (which would start the request over) nor open
// (which would refuse it). The next resume continues the same conversation.
func TestAnAttemptThatNeverArrivedLeavesTheJournalAsItFoundIt(t *testing.T) {
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_attempts", "mock/model")}
	main := &scriptedAttemptsMain{attempts: []attemptScript{
		{closed: true, stream: "half an answer", err: errors.New("the vendor stopped")},
		{neverStarted: true, err: errors.New("claude could not start")},
		{closed: true},
	}}
	opts := Options{Backend: main, Model: "mock/model", Mode: ModeCode, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard}
	first := New(opts)
	if err := first.RunTurn(context.Background(), "do the thing"); err == nil {
		t.Fatal("the vendor's stop did not stop the turn")
	}
	_ = first.Close()
	for attempt, wantErr := range []bool{true, false} {
		next := New(opts)
		pending, ok := next.Resume()
		if !ok {
			t.Fatalf("resume %d: nothing to resume", attempt+1)
		}
		err := next.RunTurn(context.Background(), pending)
		_ = next.Close()
		if (err != nil) != wantErr {
			t.Fatalf("resume %d = %v; want error %v", attempt+1, err, wantErr)
		}
		if attempt == 0 {
			run := sess.RunState()
			if run == nil || run.Main.ProviderNeverStarted || !run.Main.ProviderDelivered || !run.Main.ProviderTurnClosed || !run.Main.ProviderInFlight ||
				run.Main.PartialOutput != "half an answer" {
				t.Fatalf("after an attempt that never arrived the journal says %+v; want the delivered, closed, interrupted turn it had", run.Main)
			}
		}
	}
	main.mu.Lock()
	defer main.mu.Unlock()
	if main.calls != 3 {
		t.Fatalf("calls = %d, want three", main.calls)
	}
	for i := 1; i < 3; i++ {
		if last := main.sent[i][len(main.sent[i])-1].Content; !strings.Contains(last, "Continue this unfinished request") {
			t.Fatalf("call %d ended with %q; want a continuation of the same conversation", i+1, last)
		}
	}
}

type unresumableAttemptsMain struct{ *scriptedAttemptsMain }

func (unresumableAttemptsMain) ResumesConversation() bool { return false }

// An attempt that never arrived after a committed turn leaves the turn
// committed: an agent-mode request whose plan the vendor answered, and whose
// synthesis could not start, resumes as committed work. Nothing of the
// vendor's is open, so it needs no continuation proof, even from an adapter
// that cannot resume a conversation.
func TestACommittedTurnStaysCommittedWhenTheNextAttemptNeverArrives(t *testing.T) {
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_committed_attempt", "parent")}
	main := &scriptedAttemptsMain{attempts: []attemptScript{
		{closed: true, reply: `[{"title":"first","kind":"explain"},{"title":"second","kind":"explain"}]`},
		{neverStarted: true, err: errors.New("claude could not start")},
		{closed: true, reply: "all done"},
	}}
	opts := Options{Backend: unresumableAttemptsMain{main}, Model: "parent", Mode: ModeAgent, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard, MaxConcurrentTasks: 1,
		SubagentBackend: func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
			return childCaptureBackend{text: "task done"}, nil
		}}
	first := New(opts)
	if err := first.RunTurn(context.Background(), "two things"); err == nil {
		t.Fatal("the synthesis that could not start did not stop the turn")
	}
	_ = first.Close()
	if run := sess.RunState(); run == nil || run.Main.ProviderInFlight || run.Main.ProviderNeverStarted || !run.Main.ProviderDelivered {
		t.Fatalf("after the synthesis never arrived the main session is %+v; want its committed plan turn", run.Main)
	}
	second := New(opts)
	defer second.Close()
	pending, ok := second.Resume()
	if !ok {
		t.Fatal("nothing to resume")
	}
	if err := second.RunTurn(context.Background(), pending); err != nil {
		t.Fatalf("a committed turn was not resumed: %v", err)
	}
}

// handleAttemptsMain is a scripted vendor main whose handle can change
// between attempts, as an adapter's can when it mints a new one.
type handleAttemptsMain struct {
	*scriptedAttemptsMain
	handles []string
}

func (m handleAttemptsMain) ProviderHandle() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.handles[min(max(m.calls-1, 0), len(m.handles)-1)]
}

// Found by the round-3 verifier. An attempt that never arrived changes
// nothing, the conversation included: whatever handle the adapter holds
// afterwards, the journal keeps the one the request was on.
func TestAnAttemptThatNeverArrivedKeepsTheConversation(t *testing.T) {
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_keeps_handle", "mock/model")}
	main := handleAttemptsMain{scriptedAttemptsMain: &scriptedAttemptsMain{attempts: []attemptScript{
		{closed: true, err: errors.New("the vendor stopped")},
		{neverStarted: true, err: errors.New("claude could not start")},
	}}, handles: []string{"saved-h", "minted-after"}}
	opts := Options{Backend: main, Model: "mock/model", Mode: ModeCode, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard}
	first := New(opts)
	if err := first.RunTurn(context.Background(), "do the thing"); err == nil {
		t.Fatal("the vendor's stop did not stop the turn")
	}
	_ = first.Close()
	if run := sess.RunState(); run == nil || run.Main.ProviderState != "saved-h" {
		t.Fatalf("setup: saved main = %+v", run)
	}
	main.handles = []string{"saved-h", "minted-after"}
	second := New(opts)
	pending, ok := second.Resume()
	if !ok {
		t.Fatal("nothing to resume")
	}
	// The ownership check sees the adapter still on saved-h; the attempt then
	// never arrives and leaves the adapter holding a new handle.
	_ = second.RunTurn(context.Background(), pending)
	_ = second.Close()
	if run := sess.RunState(); run == nil || run.Main.ProviderState != "saved-h" || !run.Main.ProviderConfirmed {
		t.Fatalf("after an attempt that never arrived the journal is on %+v; want the conversation it was saved on", run.Main)
	}
}

// mintingVendorChild drives its saved conversation until its call is made,
// then holds a handle it minted, as an adapter can after a process that never
// started.
type mintingVendorChild struct {
	resumableVendorChild
	minted string
	called *atomic.Bool
}

func (c mintingVendorChild) StreamChat(ctx context.Context, model string, msgs []provider.Message, tools []provider.Tool, onToken func(string)) (provider.Message, provider.Meta, error) {
	return c.StreamChatObserved(ctx, model, msgs, tools, onToken, nil)
}

func (c mintingVendorChild) StreamChatObserved(ctx context.Context, model string, msgs []provider.Message, tools []provider.Tool, onToken func(string), observe func(provider.ProgressEvent)) (provider.Message, provider.Meta, error) {
	c.called.Store(true)
	return c.resumableVendorChild.StreamChatObserved(ctx, model, msgs, tools, onToken, observe)
}

func (c mintingVendorChild) ProviderHandle() string {
	if c.called.Load() {
		return c.minted
	}
	return c.handle
}

// The same holds for a child: a continuation that never arrived leaves the
// task on the conversation it was saved on, confirmation included.
func TestAChildAttemptThatNeverArrivedKeepsItsConversation(t *testing.T) {
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_child_keeps_handle", "parent")}
	opts := Options{Backend: reviewPlanner{titles: []string{"first", "second"}}, Model: "parent", Mode: ModeAgent, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard, MaxConcurrentTasks: 1}
	opts.SubagentBackend = func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
		return resumableVendorChild{vendorChild{handle: "saved-h", confirmed: true, closed: true, err: vendorLimit}}, nil
	}
	first := New(opts)
	var paused *PausedError
	if err := first.RunTurn(context.Background(), "two things"); !errors.As(err, &paused) {
		t.Fatalf("setup: not paused: %v", err)
	}
	_ = first.Close()
	if run := sess.RunState(); run == nil || run.Tasks[0].ProviderState != "saved-h" || !run.Tasks[0].ProviderConfirmed {
		t.Fatalf("setup: saved task = %+v", run)
	}
	var called atomic.Bool
	opts.SubagentBackend = func(_ context.Context, _, _, _ string, caps SubagentCapabilities) (ChatBackend, error) {
		if caps.ProviderState == "saved-h" {
			return mintingVendorChild{resumableVendorChild{vendorChild{handle: "saved-h", neverStarted: true, err: errors.New("claude could not start")}}, "minted-after", &called}, nil
		}
		return childCaptureBackend{text: "task done"}, nil
	}
	second := New(opts)
	pending, ok := second.Resume()
	if !ok {
		t.Fatal("nothing to resume")
	}
	_ = second.RunTurn(context.Background(), pending)
	_ = second.Close()
	if !called.Load() {
		t.Fatal("setup: the continuation was never attempted")
	}
	if run := sess.RunState(); run == nil || run.Tasks[0].ProviderState != "saved-h" || !run.Tasks[0].ProviderConfirmed {
		t.Fatalf("after a continuation that never arrived the task is on %+v; want the conversation it was saved on", run.Tasks[0])
	}
}

// Found by the round-4 verifier. Every request journals the session's vendor
// conversation, and only one whose vendor call stopped is a continuation: an
// ordinary second request is sent as itself.
func TestAFreshRequestOnAVendorConversationIsNotAContinuation(t *testing.T) {
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_fresh_request", "mock/model")}
	sess.SetProviderStateName("main-h")
	main := &scriptedAttemptsMain{attempts: []attemptScript{{closed: true, reply: "one"}, {closed: true, reply: "two"}}}
	agent := New(Options{Backend: main, Model: "mock/model", Mode: ModeCode, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard})
	defer agent.Close()
	for _, request := range []string{"first request", "second request"} {
		if err := agent.RunTurn(context.Background(), request); err != nil {
			t.Fatal(err)
		}
	}
	main.mu.Lock()
	defer main.mu.Unlock()
	if main.calls < 2 {
		t.Fatalf("calls = %d, want both requests sent", main.calls)
	}
	if last := main.sent[1][len(main.sent[1])-1].Content; last != "second request" {
		t.Fatalf("the second request ended with %q; want the request itself, not a continuation", last)
	}
	for i, sent := range main.sent {
		for _, message := range sent {
			if strings.Contains(message.Content, "Continue this unfinished request") {
				t.Fatalf("call %d carried a continuation although nothing stopped", i+1)
			}
		}
	}
}

type forgettingMain struct {
	*scriptedAttemptsMain
	forgot atomic.Int32
}

func (m *forgettingMain) ForgetConversation() { m.forgot.Add(1) }

// Found by the round-4 verifier. /new keeps the backend, but not its vendor
// conversation: that belonged to the old session, whose saved work may wait
// there.
func TestANewSessionLeavesTheOldSessionsConversation(t *testing.T) {
	main := &forgettingMain{scriptedAttemptsMain: &scriptedAttemptsMain{attempts: []attemptScript{{closed: true}}}}
	agent := New(Options{Backend: main, Model: "mock/model", Mode: ModeCode, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: enginetest.NewFakeSession("s_old", "mock/model"), Root: t.TempDir(), Out: io.Discard})
	defer agent.Close()
	agent.ReplaceSession(enginetest.NewFakeSession("s_new", "mock/model"), nil)
	if main.forgot.Load() != 1 {
		t.Fatalf("ForgetConversation called %d times, want once", main.forgot.Load())
	}
}

// A run saved in its plan phase before the planner call was sent (nothing in
// flight) plans afresh on resume: there is nothing to continue, and a
// continuation would tell the vendor it had started something it never saw.
func TestAPlannerThatWasNeverSentIsNotContinued(t *testing.T) {
	plan := `[{"title":"first","kind":"explain"},{"title":"second","kind":"explain"}]`
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_planner_unsent", "parent")}
	main := &scriptedAttemptsMain{attempts: []attemptScript{{closed: true, err: round5Limit()}, {closed: true, reply: plan}, {closed: true, reply: "all done"}}}
	opts := Options{Backend: main, Model: "parent", Mode: ModeAgent, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard, MaxConcurrentTasks: 1,
		SubagentBackend: func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
			return childCaptureBackend{text: "task done"}, nil
		}}
	first := New(opts)
	var paused *PausedError
	if err := first.RunTurn(context.Background(), "two things"); !errors.As(err, &paused) {
		t.Fatalf("setup: not paused: %v", err)
	}
	_ = first.Close()
	run := sess.RunState()
	if run.Phase != "plan" {
		t.Fatalf("setup: saved in phase %q", run.Phase)
	}
	// The journal of a stop before the planner call left: nothing in flight.
	run.Main.ProviderInFlight, run.Main.ProviderTurnClosed = false, false
	sess.SetRunState(run)
	second := New(opts)
	defer second.Close()
	pending, ok := second.Resume()
	if !ok {
		t.Fatal("nothing to resume")
	}
	if err := second.RunTurn(context.Background(), pending); err != nil {
		t.Fatal(err)
	}
	main.mu.Lock()
	defer main.mu.Unlock()
	for i, sent := range main.sent[1:] {
		if last := sent[len(sent)-1].Content; strings.Contains(last, "Continue this unfinished request") {
			t.Fatalf("call %d after resume was sent as a continuation, although nothing was in flight", i+2)
		}
	}
}

// nativeAttemptsMain is the same scripted main without a vendor conversation:
// kolk's own provider, which keeps no handle.
type nativeAttemptsMain struct{ m *scriptedAttemptsMain }

func (n nativeAttemptsMain) StreamChat(ctx context.Context, model string, msgs []provider.Message, tools []provider.Tool, onToken func(string)) (provider.Message, provider.Meta, error) {
	return n.m.StreamChat(ctx, model, msgs, tools, onToken)
}

// Found as surviving mutants by the round-6 verifier. Only a vendor
// conversation is continued: a native main stopped while planning or
// synthesizing has nothing on the provider's side to continue, so its call
// is made again as itself, never with the vendor continuation message.
func TestANativeMainIsNeverSentAVendorContinuation(t *testing.T) {
	plan := `[{"title":"first","kind":"explain"},{"title":"second","kind":"explain"}]`
	for _, stopAt := range []string{"planner", "synthesis"} {
		t.Run(stopAt, func(t *testing.T) {
			attempts := []attemptScript{{err: round5Limit()}, {reply: plan}, {reply: "all done"}}
			if stopAt == "synthesis" {
				attempts = []attemptScript{{reply: plan}, {err: round5Limit()}, {reply: "all done"}}
			}
			sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_native_phase", "parent")}
			main := &scriptedAttemptsMain{attempts: attempts}
			opts := Options{Backend: nativeAttemptsMain{main}, Model: "parent", Mode: ModeAgent, Effort: EffortMedium,
				Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard, MaxConcurrentTasks: 1,
				SubagentBackend: func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
					return childCaptureBackend{text: "task done"}, nil
				}}
			first := New(opts)
			var paused *PausedError
			if err := first.RunTurn(context.Background(), "two things"); !errors.As(err, &paused) {
				t.Fatalf("setup: not paused: %v", err)
			}
			_ = first.Close()
			if run := sess.RunState(); run == nil || !run.Main.ProviderInFlight || run.Main.ProviderState != "" {
				t.Fatalf("setup: saved main = %+v; want a native call in flight", run)
			}
			second := New(opts)
			defer second.Close()
			pending, ok := second.Resume()
			if !ok {
				t.Fatal("nothing to resume")
			}
			if err := second.RunTurn(context.Background(), pending); err != nil {
				t.Fatal(err)
			}
			main.mu.Lock()
			defer main.mu.Unlock()
			for i, sent := range main.sent {
				if last := sent[len(sent)-1].Content; strings.Contains(last, "Continue this unfinished request") {
					t.Fatalf("call %d to a native main carried the vendor continuation", i+1)
				}
			}
		})
	}
}
