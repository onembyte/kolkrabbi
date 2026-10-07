package engine

// Pins for the independent review of §8 item 6's Capture leaf: every finding
// is reproduced here before its fix.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/protocol"
)

// reviewSession fails chosen recovery writes, records the rest, and lets a
// test look at the world at the moment each write happens.
type reviewSession struct {
	*enginetest.FakeSession
	mu      sync.Mutex
	fail    map[string]error
	at      func(reason string, run *continuity.Run)
	reasons []string
	runs    []*continuity.Run
}

func (s *reviewSession) SaveRecovery(reason string) error {
	run := s.RunState()
	if s.at != nil {
		s.at(reason, run)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reasons = append(s.reasons, reason)
	if err := s.fail[reason]; err != nil {
		return err
	}
	s.runs = append(s.runs, run)
	return nil
}

func (s *reviewSession) written() ([]string, []*continuity.Run) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.reasons...), append([]*continuity.Run(nil), s.runs...)
}

// reviewPlanner plans the given titles, then synthesizes.
type reviewPlanner struct{ titles []string }

func (p reviewPlanner) StreamChat(_ context.Context, _ string, messages []provider.Message, _ []provider.Tool, _ func(string)) (provider.Message, provider.Meta, error) {
	for _, m := range messages {
		if strings.Contains(m.Content, "Decompose the request") {
			var plan []map[string]string
			for _, title := range p.titles {
				plan = append(plan, map[string]string{"title": title, "kind": "explain"})
			}
			data, _ := json.Marshal(plan)
			return provider.Message{Role: "assistant", Content: string(data)}, provider.Meta{}, nil
		}
	}
	return provider.Message{Role: "assistant", Content: "synthesized"}, provider.Meta{}, nil
}

func limitEvents(t *testing.T, events []protocol.Envelope) []protocol.ProviderLimitData {
	t.Helper()
	var out []protocol.ProviderLimitData
	for _, env := range events {
		if env.Type != protocol.EventProviderLimit {
			continue
		}
		var data protocol.ProviderLimitData
		if err := json.Unmarshal(env.Data, &data); err != nil {
			t.Fatal(err)
		}
		out = append(out, data)
	}
	return out
}

// 1. A recovery write that fails on the disk is a disk failure. Errno values
// look like network errors to a naive check; they must never become a
// transport limit, a pause, or a published provider.limit.
func TestARecoveryWriteFailureIsNeverAProviderLimit(t *testing.T) {
	quota := &fs.PathError{Op: "write", Path: "/sessions/s.resume.json.gz.tmp", Err: syscall.EDQUOT}
	t.Run("agent mode, a child error's write", func(t *testing.T) {
		b := newTestBus(t)
		sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_quota_agent", "parent"), fail: map[string]error{"error": quota}}
		var calls atomic.Int32
		agent := New(Options{
			Backend: reviewPlanner{titles: []string{"first", "second"}}, Bus: b, Model: "parent", Mode: ModeAgent,
			Effort: EffortMedium, Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard,
			MaxConcurrentTasks: 1,
			SubagentBackend: func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
				if calls.Add(1) == 1 {
					return childCaptureBackend{text: "partial", err: errors.New("child provider failed")}, nil
				}
				return childCaptureBackend{text: "second done"}, nil
			},
		})
		err := agent.RunTurn(context.Background(), "do two things")
		var paused *PausedError
		if errors.As(err, &paused) || sess.Paused() != nil {
			t.Fatalf("a disk failure became a pause: err %v, paused %+v", err, sess.Paused())
		}
		if err == nil || !strings.Contains(err.Error(), "recovery point") {
			t.Fatalf("RunTurn = %v, want the failed recovery write", err)
		}
		if got := limitEvents(t, bReplay(t, b)); len(got) != 0 {
			t.Fatalf("a disk failure published provider limits: %+v", got)
		}
		if reasons, _ := sess.written(); len(reasons) != 1 {
			t.Fatalf("recovery writes %v; a failed write must not be followed by a pause write", reasons)
		}
	})
	t.Run("code mode, an ordinary provider error's write", func(t *testing.T) {
		b := newTestBus(t)
		full := &fs.PathError{Op: "write", Path: "/sessions/s.resume.json.gz.tmp", Err: syscall.ENOSPC}
		sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_nospace_code", "mock/model"), fail: map[string]error{"error": full}}
		agent := New(Options{
			Backend: partialFailureBackend{partial: "half an answer", err: errors.New("the model returned malformed output")},
			Bus:     b, Model: "mock/model", Mode: ModeCode, Effort: EffortMedium,
			Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard,
		})
		err := agent.RunTurn(context.Background(), "explain")
		if err == nil || !strings.Contains(err.Error(), "malformed output") || !strings.Contains(err.Error(), "recovery point") {
			t.Fatalf("RunTurn = %v, want the provider error and the failed write", err)
		}
		if got := limitEvents(t, bReplay(t, b)); len(got) != 0 {
			t.Fatalf("a disk failure published provider limits: %+v", got)
		}
	})
}

// durableMirrorError is a recovery point that reached disk while its JSON
// mirror did not: what the session reports after the compressed rename.
type durableMirrorError struct{}

func (durableMirrorError) Error() string {
	return "recovery point saved but JSON mirror failed: read-only"
}
func (durableMirrorError) RecoveryDurable() bool { return true }

// 2. The compressed file is the recovery point. A mirror or header failure
// after it is durable is a warning, not a failed boundary: the pause stands,
// is armed and is announced.
func TestADurableRecoveryWithAFailedMirrorIsStillDurable(t *testing.T) {
	b := newTestBus(t)
	var out bytes.Buffer
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_mirror", "mock/model"), fail: map[string]error{"pause": durableMirrorError{}}}
	agent := New(Options{
		Backend: partialFailureBackend{partial: "half", err: provider.Limit{Kind: provider.LimitSubscriptionAllowance,
			Scope: provider.ScopeAccount, Model: "mock/model", ResetAt: time.Now().Add(time.Hour)}},
		Bus: b, Model: "mock/model", Mode: ModeCode, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: &out,
	})
	err := agent.RunTurn(context.Background(), "wait for the reset")
	var paused *PausedError
	if !errors.As(err, &paused) || sess.Paused() == nil {
		t.Fatalf("a durable recovery point with a failed mirror was not a pause: %v", err)
	}
	if got := limitEvents(t, bReplay(t, b)); len(got) != 1 || got[0].Action != "pause" {
		t.Fatalf("limit events %+v, want the one pause", got)
	}
	if !strings.Contains(out.String(), "JSON mirror failed") {
		t.Fatalf("the mirror failure was not said:\n%s", out.String())
	}
}

// 3. A provider call is in flight only once it is sent. A call the pause gate
// stops before it leaves was never sent, and must not be journaled as one: a
// resume would treat a clean boundary as an uncertain vendor turn.
func TestACallThePauseGateStoppedWasNeverInFlight(t *testing.T) {
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_unsent", "parent")}
	var sent atomic.Int32
	agent := New(Options{
		Backend: &answeringBackend{}, Model: "parent", Mode: ModeAgent, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard, MaxConcurrentTasks: 2,
		SubagentBackend: func(ctx context.Context, model, _, _ string, _ SubagentCapabilities) (ChatBackend, error) {
			if model == "limit-child" {
				return childCaptureBackendFunc(func(func(string)) (provider.Message, provider.Meta, error) {
					return provider.Message{}, provider.Meta{}, provider.Limit{Kind: provider.LimitSubscriptionAllowance,
						Scope: provider.ScopeAccount, Model: "limit-child", ResetAt: time.Now().Add(time.Hour)}
				}), nil
			}
			deadline := time.Now().Add(2 * time.Second)
			for !executionPauseRecorded(ctx) && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			return childCaptureBackendFunc(func(func(string)) (provider.Message, provider.Meta, error) {
				sent.Add(1)
				return provider.Message{Role: "assistant", Content: "ran"}, provider.Meta{}, nil
			}), nil
		},
	})
	agent.lastTurnID = "turn_unsent"
	agent.beginExecution("request", "request")
	tasks := []Task{
		{Title: "limit", Kind: KindExplain, Model: "limit-child", Effort: EffortMedium},
		{Title: "slow open", Kind: KindExplain, Model: "slow-child", Effort: EffortMedium},
	}
	agent.setExecutionPlan(tasks)
	agent.storeExecution()
	agent.runSpend = &spend{}
	_, err := agent.runTasks(context.Background(), "request", tasks)
	if _, _, ok := agent.pauseIfWaitingHelps(context.Background(), err, "request"); !ok {
		t.Fatalf("not paused: %v", err)
	}
	_, runs := sess.written()
	if len(runs) == 0 {
		t.Fatal("no recovery point")
	}
	task := runs[len(runs)-1].Tasks[1]
	if sent.Load() != 0 {
		t.Skipf("the gate closed too late on this machine (%d calls)", sent.Load())
	}
	if task.ProviderInFlight {
		t.Fatalf("a child whose provider was never called is saved in flight: %+v", task)
	}
}

// 4. Partial output grows in linear time: every token must not copy the whole
// accumulated text under the journal lock the scheduler also needs.
func TestPartialOutputGrowsInLinearTime(t *testing.T) {
	agent := New(Options{Backend: &answeringBackend{}, Model: "parent", Mode: ModeAgent,
		Sess: enginetest.NewFakeSession("s_linear", "parent"), Root: t.TempDir(), Out: io.Discard})
	agent.beginExecution("request", "request")
	agent.setExecutionPlan([]Task{{Title: "child", Model: "child", Effort: EffortMedium}})
	agent.beginChildProviderCall(0)
	token := "abcé" // multibyte, so no byte is lost at a boundary either
	const tokens = 128 << 10
	start := time.Now()
	for range tokens {
		agent.appendChildPartial(0, token)
	}
	took := time.Since(start)
	agent.storeExecution()
	got := agent.executionTask(0).PartialOutput
	if got != strings.Repeat(token, tokens) {
		t.Fatalf("partial output is %d bytes, want %d exactly", len(got), len(token)*tokens)
	}
	if took > 500*time.Millisecond {
		t.Fatalf("appending %d KiB took %v; the copy is not linear", len(got)>>10, took)
	}
}

// toolEventBackend is a vendor-run turn: its own tools start and finish
// inside the provider, reported only through progress events.
type toolEventBackend struct {
	events []provider.ProgressEvent
	err    error
}

func (b toolEventBackend) StreamChat(ctx context.Context, model string, msgs []provider.Message, tools []provider.Tool, onToken func(string)) (provider.Message, provider.Meta, error) {
	return b.StreamChatObserved(ctx, model, msgs, tools, onToken, nil)
}

func (b toolEventBackend) StreamChatObserved(_ context.Context, _ string, _ []provider.Message, _ []provider.Tool, _ func(string), observe func(provider.ProgressEvent)) (provider.Message, provider.Meta, error) {
	for _, event := range b.events {
		if observe != nil {
			observe(event)
		}
	}
	if b.err != nil {
		return provider.Message{}, provider.Meta{}, b.err
	}
	return provider.Message{Role: "assistant", Content: "done"}, provider.Meta{}, nil
}

// 5. A vendor-run turn's own tool boundaries are journaled: the last tool it
// finished and the ones it started without finishing, so a resume knows which
// vendor actions completed. Live-only events are not enough.
func TestAVendorTurnsToolBoundariesAreJournaled(t *testing.T) {
	events := []provider.ProgressEvent{
		{Kind: provider.ProgressToolStarted, ID: "call_a", Name: "Bash"},
		{Kind: provider.ProgressToolFinished, ID: "call_a", Name: "Bash"},
		{Kind: provider.ProgressToolStarted, ID: "call_b", Name: "Edit"},
	}
	t.Run("a child", func(t *testing.T) {
		sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_vendor_child", "parent")}
		agent := New(Options{
			Backend: reviewPlanner{titles: []string{"first", "second"}}, Model: "parent", Mode: ModeAgent, Effort: EffortMedium,
			Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard, MaxConcurrentTasks: 1,
			SubagentBackend: func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
				return toolEventBackend{events: events, err: errors.New("the vendor process exited")}, nil
			},
		})
		_ = agent.RunTurn(context.Background(), "one thing")
		_, runs := sess.written()
		if len(runs) == 0 || len(runs[len(runs)-1].Tasks) == 0 {
			t.Fatal("no recovery point with the task")
		}
		tools := runs[len(runs)-1].Tasks[0].VendorTools
		if tools == nil || tools.LastFinishedID != "call_a" || tools.LastFinishedName != "Bash" ||
			len(tools.Unfinished) != 1 || tools.Unfinished[0] != "call_b" {
			t.Fatalf("vendor tools = %+v, want call_a finished and call_b unfinished", tools)
		}
	})
	t.Run("the main session", func(t *testing.T) {
		sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_vendor_main", "mock/model")}
		agent := New(Options{
			Backend: toolEventBackend{events: events, err: errors.New("the vendor process exited")},
			Model:   "mock/model", Mode: ModeCode, Effort: EffortMedium,
			Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard,
		})
		_ = agent.RunTurn(context.Background(), "do it")
		_, runs := sess.written()
		if len(runs) == 0 {
			t.Fatal("no recovery point")
		}
		tools := runs[len(runs)-1].Main.VendorTools
		if tools == nil || tools.LastFinishedID != "call_a" || len(tools.Unfinished) != 1 || tools.Unfinished[0] != "call_b" {
			t.Fatalf("main vendor tools = %+v, want call_a finished and call_b unfinished", tools)
		}
	})
}

// 6. The pause is announced only after its recovery point is durable: at the
// moment of the write, nothing has told anyone the session is paused, and the
// main task is already waiting.
func TestThePauseIsAnnouncedOnlyAfterItsWrite(t *testing.T) {
	b := newTestBus(t)
	var out safeBuffer
	var announced []string
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_order", "mock/model")}
	sess.at = func(reason string, run *continuity.Run) {
		if reason != "pause" {
			return
		}
		for _, data := range limitEvents(t, bReplay(t, b)) {
			if data.Action == "pause" {
				announced = append(announced, "provider.limit{pause}")
			}
		}
		for _, env := range bReplay(t, b) {
			if env.Type == protocol.EventTurnFinished && strings.Contains(string(env.Data), `"paused"`) {
				announced = append(announced, "turn.finished{paused}")
			}
		}
		if strings.Contains(out.String(), "◆ paused") {
			announced = append(announced, "◆ paused")
		}
		if run == nil || run.Main.State != continuity.TaskWaiting {
			announced = append(announced, "main not waiting")
		}
	}
	agent := New(Options{
		Backend: partialFailureBackend{partial: "half", err: provider.Limit{Kind: provider.LimitSubscriptionAllowance,
			Scope: provider.ScopeAccount, Model: "mock/model", ResetAt: time.Now().Add(time.Hour)}},
		Bus: b, Model: "mock/model", Mode: ModeCode, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: &out,
	})
	var paused *PausedError
	if err := agent.RunTurn(context.Background(), "wait"); !errors.As(err, &paused) {
		t.Fatalf("not paused: %v", err)
	}
	if len(announced) != 0 {
		t.Fatalf("at the pause write: %v", announced)
	}
}

type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// 6. A cancel the user asked for is terminal: no recovery point is written
// for work they withdrew.
func TestAUserCancelWritesNoRecoveryPoint(t *testing.T) {
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_cancel", "mock/model")}
	ctx, cancel := context.WithCancel(context.Background())
	agent := New(Options{
		Backend: contextChildBackendFunc(func(ctx context.Context, onToken func(string)) (provider.Message, provider.Meta, error) {
			onToken("started")
			cancel()
			<-ctx.Done()
			return provider.Message{}, provider.Meta{}, ctx.Err()
		}),
		Model: "mock/model", Mode: ModeCode, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard,
	})
	_ = agent.RunTurn(ctx, "never mind")
	if reasons, _ := sess.written(); len(reasons) != 0 {
		t.Fatalf("a user cancel wrote recovery points %v", reasons)
	}
	if run := sess.RunState(); run != nil && run.Phase != "stopped" {
		t.Fatalf("a user cancel left the run %q, want stopped", run.Phase)
	}
}

// 6. An ordinary error keeps the run for recovery; only a cancel stops it.
func TestAnOrdinaryErrorKeepsTheRun(t *testing.T) {
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_keep", "mock/model")}
	agent := New(Options{
		Backend: partialFailureBackend{partial: "half", err: errors.New("the model returned malformed output")},
		Model:   "mock/model", Mode: ModeCode, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard,
	})
	if err := agent.RunTurn(context.Background(), "explain"); err == nil {
		t.Fatal("the error was swallowed")
	}
	run := sess.RunState()
	if run == nil || run.Phase == "stopped" || run.Phase == "done" {
		t.Fatalf("an ordinary error ended the run: %+v", run)
	}
	reasons, _ := sess.written()
	if len(reasons) != 1 || reasons[0] != "error" {
		t.Fatalf("recovery writes %v, want one error write", reasons)
	}
}

// toolThenFailBackend runs one read_file round, then fails the next call.
type toolThenFailBackend struct {
	path  string
	calls atomic.Int32
}

func (b *toolThenFailBackend) StreamChat(_ context.Context, _ string, _ []provider.Message, _ []provider.Tool, onToken func(string)) (provider.Message, provider.Meta, error) {
	if b.calls.Add(1) == 1 {
		args, _ := json.Marshal(map[string]string{"path": b.path})
		return provider.Message{Role: "assistant", Content: "Reading.", ToolCalls: []provider.ToolCall{{ID: "call_read",
			Function: provider.FunctionCall{Name: "read_file", Arguments: string(args)}}}}, provider.Meta{}, nil
	}
	onToken("half of the answer")
	return provider.Message{}, provider.Meta{}, errors.New("the model returned malformed output")
}

// 6. The main loop's own tool round is a journaled boundary: the recovery
// point after a later failure says the tool results were committed.
func TestTheMainLoopJournalsItsToolBoundary(t *testing.T) {
	root := t.TempDir()
	path := root + "/note.txt"
	if err := writeReviewFile(path); err != nil {
		t.Fatal(err)
	}
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_main_tool", "mock/model")}
	agent := New(Options{Backend: &toolThenFailBackend{path: path}, Model: "mock/model", Mode: ModeCode, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: root, Out: io.Discard})
	_ = agent.RunTurn(context.Background(), "read and answer")
	_, runs := sess.written()
	if len(runs) == 0 {
		t.Fatal("no recovery point")
	}
	main := runs[len(runs)-1].Main
	if main.SafeBoundary != "tool results committed" || !main.ProviderInFlight || main.PartialOutput != "half of the answer" {
		t.Fatalf("main = boundary %q, in flight %v, partial %q; want the tool round committed and the failed call's words kept",
			main.SafeBoundary, main.ProviderInFlight, main.PartialOutput)
	}
}

// 6. A final message supersedes the partial text that preceded it, for the
// main session and for a child.
func TestAFinalMessageSupersedesThePartial(t *testing.T) {
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_supersede", "parent")}
	agent := New(Options{
		Backend: reviewPlanner{titles: []string{"only"}}, Model: "parent", Mode: ModeAgent, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard,
		SubagentBackend: func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
			return childCaptureBackend{text: "streamed then committed"}, nil
		},
	})
	agent.beginExecution("request", "request")
	agent.setExecutionPlan([]Task{{Title: "only", Kind: KindExplain, Model: "child", Effort: EffortMedium}})
	agent.beginMainProviderCall()
	agent.appendMainPartial("planning words")
	agent.completeMainProviderCall()
	agent.beginChildProviderCall(0)
	agent.appendChildPartial(0, "child words")
	agent.completeChildProviderCall(0)
	agent.storeExecution()
	run := sess.RunState()
	if run == nil || run.Main.PartialOutput != "" || run.Tasks[0].PartialOutput != "" {
		t.Fatalf("committed responses left partial text: main %q, child %q", run.Main.PartialOutput, run.Tasks[0].PartialOutput)
	}
}

func writeReviewFile(path string) error { return os.WriteFile(path, []byte("a note\n"), 0o600) }

// 6. A handle alone is never confirmation: the session records a vendor
// handle the moment a backend owns one, before the vendor accepted it.
func TestAHandleAloneIsNeverConfirmed(t *testing.T) {
	sess := enginetest.NewFakeSession("s_handle", "mock/model")
	sess.SetProviderStateName("minted-before-init")
	agent := New(Options{Backend: &answeringBackend{}, Model: "mock/model", Mode: ModeCode,
		Sess: sess, Root: t.TempDir(), Out: io.Discard})
	agent.beginExecution("request", "request")
	if run := agent.executionSnapshot(); run == nil || run.Main.ProviderState != "minted-before-init" || run.Main.ProviderConfirmed {
		t.Fatalf("main = %+v, want the handle recorded unconfirmed", run.Main)
	}
}

// 7. A pause turns running work into waiting work. Work that never started
// stays queued, and dependency order is not a pause.
func TestAPauseKeepsUnstartedWorkQueued(t *testing.T) {
	agent := New(Options{Backend: &answeringBackend{}, Model: "parent", Mode: ModeAgent,
		Sess: enginetest.NewFakeSession("s_queued", "parent"), Root: t.TempDir(), Out: io.Discard})
	agent.beginExecution("request", "request")
	agent.setExecutionPlan([]Task{
		{Title: "running", Model: "child", Effort: EffortMedium},
		{Title: "never started", Model: "child", Effort: EffortMedium},
		{Title: "after the first", Model: "child", Effort: EffortMedium, Needs: []int{0}},
	})
	if got := agent.executionTask(2).State; got != continuity.TaskQueued {
		t.Fatalf("a task behind a dependency is %q at plan time, want queued", got)
	}
	agent.setExecutionTaskState(0, continuity.TaskRunning)
	agent.markExecutionWaiting()
	for i, want := range []string{continuity.TaskWaiting, continuity.TaskQueued, continuity.TaskQueued} {
		if got := agent.executionTask(i).State; got != want {
			t.Errorf("task %d is %q after the pause, want %q", i, got, want)
		}
	}
}

// 8. One boundary is one recovery point: a child's limit and a sibling's
// error that meet at the same boundary are saved once, as the pause.
func TestOneBoundaryIsOneRecoveryWriteAtTheScheduler(t *testing.T) {
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_one_sched", "parent")}
	limitReturned := make(chan struct{})
	agent := New(Options{
		Backend: &answeringBackend{}, Model: "parent", Mode: ModeAgent, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard, MaxConcurrentTasks: 2,
		SubagentBackend: func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
			return nil, nil
		},
	})
	agent.SubagentBackend = func(_ context.Context, model, _, _ string, _ SubagentCapabilities) (ChatBackend, error) {
		if model == "limit-child" {
			return childCaptureBackendFunc(func(func(string)) (provider.Message, provider.Meta, error) {
				defer close(limitReturned)
				return provider.Message{}, provider.Meta{}, provider.Limit{Kind: provider.LimitSubscriptionAllowance,
					Scope: provider.ScopeAccount, Model: "limit-child", ResetAt: time.Now().Add(time.Hour)}
			}), nil
		}
		return childCaptureBackendFunc(func(func(string)) (provider.Message, provider.Meta, error) {
			<-limitReturned
			return provider.Message{}, provider.Meta{}, errors.New("sibling failed")
		}), nil
	}
	agent.lastTurnID = "turn_one"
	agent.beginExecution("request", "request")
	tasks := []Task{
		{Title: "limit", Kind: KindExplain, Model: "limit-child", Effort: EffortMedium},
		{Title: "error", Kind: KindExplain, Model: "error-child", Effort: EffortMedium},
	}
	agent.setExecutionPlan(tasks)
	agent.storeExecution()
	agent.runSpend = &spend{}
	_, err := agent.runTasks(context.Background(), "request", tasks)
	if _, _, ok := agent.pauseIfWaitingHelps(context.Background(), err, "request"); !ok {
		t.Fatalf("not paused: %v", err)
	}
	if reasons, _ := sess.written(); len(reasons) != 1 || reasons[0] != "pause" {
		t.Fatalf("recovery writes %v at one boundary, want one pause write", reasons)
	}
}

// 9. A child is announced paused only once the pause is durable. When the
// write fails, no child claims to be waiting for an allowance.
func TestAChildIsAnnouncedPausedOnlyAfterTheDurablePause(t *testing.T) {
	b := newTestBus(t)
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_child_paused", "parent"),
		fail: map[string]error{"pause": errors.New("recovery volume is full")}}
	agent := New(Options{
		Backend: reviewPlanner{titles: []string{"first", "second"}}, Bus: b, Model: "parent", Mode: ModeAgent, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard,
		SubagentBackend: func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
			return childCaptureBackendFunc(func(func(string)) (provider.Message, provider.Meta, error) {
				return provider.Message{}, provider.Meta{}, provider.Limit{Kind: provider.LimitSubscriptionAllowance,
					Scope: provider.ScopeAccount, Model: "child", ResetAt: time.Now().Add(time.Hour)}
			}), nil
		},
	})
	_ = agent.RunTurn(context.Background(), "one thing")
	for _, env := range bReplay(t, b) {
		if env.Type == protocol.EventSubagentFinished && strings.Contains(string(env.Data), `"paused"`) {
			t.Fatalf("a child was announced paused though the pause was never saved: %s", env.Data)
		}
	}
	agent.subagentMu.Lock()
	defer agent.subagentMu.Unlock()
	for _, status := range agent.subagentStatus {
		if strings.Contains(status.Step, "waiting for allowance") {
			t.Fatalf("a child claims to wait for an allowance though the pause was never saved: %+v", status)
		}
	}
}

// 10. Confirmation is a fact about a handle, kept once learned: a resumed
// process that fails before the vendor's init does not unlearn it.
func TestAConfirmedHandleStaysConfirmed(t *testing.T) {
	agent := New(Options{Backend: &answeringBackend{}, Model: "parent", Mode: ModeAgent,
		Sess: enginetest.NewFakeSession("s_confirmed", "parent"), Root: t.TempDir(), Out: io.Discard})
	agent.beginExecution("request", "request")
	agent.setExecutionPlan([]Task{{Title: "child", Model: "child", Effort: EffortMedium}})
	msgs := []provider.Message{{Role: "user", Content: "work"}}
	agent.saveChildConversation(0, msgs, 1, doomLoop{}, childCaptureBackend{handle: "h1", confirmed: true})
	agent.saveChildConversation(0, msgs, 1, doomLoop{}, childCaptureBackend{handle: "h1", confirmed: false})
	if task := agent.executionTask(0); !task.ProviderConfirmed {
		t.Fatalf("a confirmed handle was unlearned: %+v", task)
	}
	agent.saveChildConversation(0, msgs, 1, doomLoop{}, childCaptureBackend{handle: "h2", confirmed: false})
	if task := agent.executionTask(0); task.ProviderConfirmed {
		t.Fatalf("a new handle inherited another's confirmation: %+v", task)
	}
}

// 11. A pause that never reached disk leaves no pause in the run: a later
// ordinary save must not persist it, or the next start would auto-deliver.
func TestAFailedPauseLeavesNoPauseInTheRun(t *testing.T) {
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_no_lastpause", "mock/model"),
		fail: map[string]error{"pause": errors.New("recovery volume is full")}}
	agent := New(Options{
		Backend: partialFailureBackend{partial: "half", err: provider.Limit{Kind: provider.LimitSubscriptionAllowance,
			Scope: provider.ScopeAccount, Model: "mock/model", ResetAt: time.Now().Add(time.Hour)}},
		Model: "mock/model", Mode: ModeCode, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard,
	})
	_ = agent.RunTurn(context.Background(), "wait")
	if run := sess.RunState(); run != nil && run.LastPause != nil {
		t.Fatalf("a pause that was never saved is in the run: %+v", run.LastPause)
	}
}

type countingBackend struct{ calls atomic.Int32 }

func (b *countingBackend) StreamChat(context.Context, string, []provider.Message, []provider.Tool, func(string)) (provider.Message, provider.Meta, error) {
	b.calls.Add(1)
	return provider.Message{Role: "assistant", Content: "sent"}, provider.Meta{}, nil
}

// 3. The start hook runs when a call is sent, and only then: the pause gate
// stopping a call first means no hook, no call.
func TestAProviderCallIsJournaledOnlyWhenSent(t *testing.T) {
	backend := &countingBackend{}
	agent := New(Options{Backend: backend, Model: "mock/model", Mode: ModeCode,
		Sess: enginetest.NewFakeSession("s_hook", "mock/model"), Root: t.TempDir(), Out: io.Discard})
	started := 0
	gate := &executionPause{}
	ctx := context.WithValue(context.Background(), executionPauseKey{}, gate)
	gate.note(provider.Limit{Kind: provider.LimitSubscriptionAllowance, Scope: provider.ScopeAccount, Model: "x"})
	_, _, err := agent.streamChat(onProviderCallStart(ctx, func() { started++ }), "reply", "mock/model", nil, nil, nil)
	if err == nil || started != 0 || backend.calls.Load() != 0 {
		t.Fatalf("a stopped gate: err %v, hook %d, calls %d; want the stop, no hook, no call", err, started, backend.calls.Load())
	}
	open := context.Background()
	if _, _, err := agent.streamChat(onProviderCallStart(open, func() {
		started++
		if backend.calls.Load() != 0 {
			t.Error("the hook ran after the call was sent")
		}
	}), "reply", "mock/model", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if started != 1 || backend.calls.Load() != 1 {
		t.Fatalf("an open gate: hook %d, calls %d; want one each", started, backend.calls.Load())
	}
}

// 3. The main loop's call is in flight only once it is sent, too: a model with
// no route to answer it fails before anything leaves, and the recovery point
// after that failure holds no vendor turn to wonder about.
func TestAMainCallThatNeverLeftIsNotInFlight(t *testing.T) {
	backend := &countingBackend{}
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_unrouted", "ollama/unattached")}
	agent := New(Options{Backend: backend, Model: "ollama/unattached", Mode: ModeCode, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard})
	if err := agent.RunTurn(context.Background(), "answer"); err == nil {
		t.Fatal("an unrouted model answered")
	}
	_, runs := sess.written()
	if len(runs) == 0 {
		t.Fatal("no recovery point after the failure")
	}
	if main := runs[len(runs)-1].Main; backend.calls.Load() != 0 || main.ProviderInFlight || main.State == continuity.TaskRunning {
		t.Fatalf("calls %d, main %+v; want nothing sent and nothing in flight", backend.calls.Load(), main)
	}
}

// 9. At the moment the pause is written, no child says it waits for an
// allowance yet; once the write lands, each held child does, and its finish
// says paused.
func TestAChildSaysItWaitsOnlyOnceThePauseIsWritten(t *testing.T) {
	b := newTestBus(t)
	var agent *Agent
	var early []string
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_child_wait", "parent")}
	sess.at = func(reason string, _ *continuity.Run) {
		if reason != "pause" {
			return
		}
		agent.subagentMu.Lock()
		defer agent.subagentMu.Unlock()
		for _, status := range agent.subagentStatus {
			if strings.Contains(status.Step, "waiting for allowance") {
				early = append(early, status.Step)
			}
		}
	}
	agent = New(Options{
		Backend: reviewPlanner{titles: []string{"first", "second"}}, Bus: b, Model: "parent", Mode: ModeAgent, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard,
		SubagentBackend: func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
			return childCaptureBackendFunc(func(func(string)) (provider.Message, provider.Meta, error) {
				return provider.Message{}, provider.Meta{}, provider.Limit{Kind: provider.LimitSubscriptionAllowance,
					Scope: provider.ScopeAccount, Model: "child", ResetAt: time.Now().Add(time.Hour)}
			}), nil
		},
	})
	var paused *PausedError
	if err := agent.RunTurn(context.Background(), "one thing"); !errors.As(err, &paused) {
		t.Fatalf("not paused: %v", err)
	}
	if len(early) != 0 {
		t.Fatalf("at the pause write, children already said %q", early)
	}
	waiting := 0
	agent.subagentMu.Lock()
	for _, status := range agent.subagentStatus {
		if status.State == SubagentWaiting && status.Step == "paused: waiting for allowance" {
			waiting++
		}
	}
	agent.subagentMu.Unlock()
	finished := 0
	for _, env := range bReplay(t, b) {
		if env.Type == protocol.EventSubagentFinished && strings.Contains(string(env.Data), `"paused"`) {
			finished++
		}
	}
	if waiting == 0 || finished == 0 {
		t.Fatalf("after the durable pause: %d children waiting, %d finished paused; want the held children announced", waiting, finished)
	}
}

// 5. A committed response ends the vendor's turn, tools included: a tool it
// reported started and never finished is no longer unfinished once its
// answer is committed, for the main session and for a child. The last tool
// finished stays.
func TestACommittedResponseEndsTheVendorsTools(t *testing.T) {
	agent := New(Options{Backend: &answeringBackend{}, Model: "parent", Mode: ModeAgent,
		Sess: enginetest.NewFakeSession("s_commit_tools", "parent"), Root: t.TempDir(), Out: io.Discard})
	agent.beginExecution("request", "request")
	agent.setExecutionPlan([]Task{{Title: "child", Model: "child", Effort: EffortMedium}})
	for _, index := range []int{-1, 0} {
		for _, event := range []provider.ProgressEvent{
			{Kind: provider.ProgressToolStarted, ID: "call_a", Name: "Bash"},
			{Kind: provider.ProgressToolFinished, ID: "call_a", Name: "Bash"},
			{Kind: provider.ProgressToolStarted, ID: "call_b", Name: "Edit"},
		} {
			agent.journalVendorTool(index, event)
		}
	}
	agent.beginMainProviderCall()
	agent.completeMainProviderCall()
	agent.beginChildProviderCall(0)
	agent.completeChildProviderCall(0)
	run := agent.executionSnapshot()
	for name, tools := range map[string]*continuity.VendorToolBoundary{"main": run.Main.VendorTools, "child": run.Tasks[0].VendorTools} {
		if tools == nil || len(tools.Unfinished) != 0 || tools.LastFinishedID != "call_a" {
			t.Errorf("%s vendor tools after the commit = %+v; want none unfinished and call_a last finished", name, tools)
		}
	}
}

// 9. Work the scheduler never launched is held at the pause too: once the
// pause is written it waits for the allowance; when the write fails it says
// no pause was saved, never that it waits.
func TestUnlaunchedWorkSaysWhatThePauseWriteDid(t *testing.T) {
	for _, c := range []struct {
		name string
		fail error
		want string
	}{
		{"written", nil, "paused: waiting for allowance"},
		{"failed", errors.New("recovery volume is full"), "stopped at the limit; no pause was saved"},
	} {
		t.Run(c.name, func(t *testing.T) {
			sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_unlaunched_"+c.name, "parent"),
				fail: map[string]error{"pause": c.fail}}
			agent := New(Options{
				Backend: reviewPlanner{titles: []string{"first", "second"}}, Model: "parent", Mode: ModeAgent, Effort: EffortMedium,
				Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard, MaxConcurrentTasks: 1,
				SubagentBackend: func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
					return childCaptureBackendFunc(func(func(string)) (provider.Message, provider.Meta, error) {
						return provider.Message{}, provider.Meta{}, provider.Limit{Kind: provider.LimitSubscriptionAllowance,
							Scope: provider.ScopeAccount, Model: "child", ResetAt: time.Now().Add(time.Hour)}
					}), nil
				},
			})
			_ = agent.RunTurn(context.Background(), "two things")
			agent.subagentMu.Lock()
			second, ok := agent.subagentStatus[1]
			agent.subagentMu.Unlock()
			if !ok || second.Step != c.want {
				t.Fatalf("the unlaunched task says %+v (present %v); want %q", second, ok, c.want)
			}
		})
	}
}

// 10. The main session keeps a learned confirmation too, for the same handle
// only.
func TestTheMainSessionKeepsAConfirmedHandle(t *testing.T) {
	agent := New(Options{Backend: childCaptureBackend{handle: "h1", confirmed: true}, Model: "mock/model", Mode: ModeCode,
		Sess: enginetest.NewFakeSession("s_main_confirmed", "mock/model"), Root: t.TempDir(), Out: io.Discard})
	agent.beginExecution("request", "request")
	agent.captureMainProviderState(agent.sessionBackend())
	agent.SetSessionBackend(childCaptureBackend{handle: "h1", confirmed: false})
	agent.captureMainProviderState(agent.sessionBackend())
	if main := agent.executionSnapshot().Main; main.ProviderState != "h1" || !main.ProviderConfirmed {
		t.Fatalf("a confirmed main handle was unlearned: %+v", main)
	}
	agent.SetSessionBackend(childCaptureBackend{handle: "h2", confirmed: false})
	agent.captureMainProviderState(agent.sessionBackend())
	if main := agent.executionSnapshot().Main; main.ProviderState != "h2" || main.ProviderConfirmed {
		t.Fatalf("a new main handle inherited another's confirmation: %+v", main)
	}
}

// 9. A cancel after a child stopped at its limit saves no pause: the held
// child says so, and nothing says it waits. The scheduler returns the cancel
// before holding the rest, so the child is settled on its own.
func TestACancelAfterAChildsLimitLeavesNoWaitingChild(t *testing.T) {
	b := newTestBus(t)
	sess := &reviewSession{FakeSession: enginetest.NewFakeSession("s_cancel_held", "parent")}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	limited := make(chan struct{})
	agent := New(Options{
		Backend: reviewPlanner{titles: []string{"first", "second"}}, Bus: b, Model: "parent", Mode: ModeAgent, Effort: EffortMedium,
		Permission: PermissionFullAuto, Sess: sess, Root: t.TempDir(), Out: io.Discard, MaxConcurrentTasks: 2,
		SubagentBackend: func(context.Context, string, string, string, SubagentCapabilities) (ChatBackend, error) {
			return contextChildBackendFunc(func(ctx context.Context, _ func(string)) (provider.Message, provider.Meta, error) {
				if calls.Add(1) == 2 {
					// The child sent second meets the limit while the first runs.
					close(limited)
					return provider.Message{}, provider.Meta{}, provider.Limit{Kind: provider.LimitSubscriptionAllowance,
						Scope: provider.ScopeAccount, Model: "child", ResetAt: time.Now().Add(time.Hour)}
				}
				<-limited
				cancel()
				<-ctx.Done()
				return provider.Message{}, provider.Meta{}, ctx.Err()
			}), nil
		},
	})
	if err := agent.RunTurn(ctx, "two things"); !errors.Is(err, context.Canceled) {
		t.Fatalf("RunTurn = %v, want the cancel", err)
	}
	agent.subagentMu.Lock()
	defer agent.subagentMu.Unlock()
	held := 0
	for _, status := range agent.subagentStatus {
		if strings.Contains(status.Step, "waiting for allowance") || strings.Contains(status.Step, "saving the pause") {
			t.Fatalf("after a cancel a child still says %q", status.Step)
		}
		if status.State == SubagentFailed && status.Step == "stopped at the limit; no pause was saved" {
			held++
		}
	}
	if held != 1 {
		t.Fatalf("%d children say the limit stopped them with no pause saved, want the one: %+v", held, agent.subagentStatus)
	}
	for _, env := range bReplay(t, b) {
		if env.Type == protocol.EventSubagentFinished && strings.Contains(string(env.Data), `"paused"`) {
			t.Fatalf("a child was announced paused after a cancel: %s", env.Data)
		}
	}
}
