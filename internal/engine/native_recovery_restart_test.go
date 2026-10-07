package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/session"
)

type nativeResumeBackend func([]provider.Message) (provider.Message, error)

func (b nativeResumeBackend) StreamChat(_ context.Context, _ string, m []provider.Message, _ []provider.Tool, _ func(string)) (provider.Message, provider.Meta, error) {
	r, e := b(m)
	return r, provider.Meta{}, e
}

func nativeRun(t *testing.T, root, phase string) *continuity.Run {
	t.Helper()
	run := &continuity.Run{Version: 1, ID: "native-recovery", Input: "finish saved work", Prompt: "finish saved work", Root: root, Mode: engine.ModeCode, Model: "mock/model", Effort: engine.EffortMedium, Phase: phase,
		Main: continuity.Task{State: continuity.TaskRunning, Model: "mock/model", Effort: engine.EffortMedium, SafeBoundary: "tool results committed", Rounds: 1}}
	// Exercise the on-disk field, including on the pre-fix struct which ignores it.
	if err := json.Unmarshal([]byte(`{"recovery":"error"}`), run); err != nil {
		t.Fatal(err)
	}
	return run
}
func recoveryReason(t *testing.T, run *continuity.Run) string {
	t.Helper()
	data, err := json.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	var reason string
	_ = json.Unmarshal(fields["recovery"], &reason)
	return reason
}
func nativeOpts(sess engine.SessionPort, root string, b engine.ChatBackend) engine.Options {
	return engine.Options{Backend: b, Model: "mock/model", Mode: engine.ModeCode, Effort: engine.EffortMedium, Permission: engine.PermissionFullAuto, Sess: sess, Root: root, Out: io.Discard}
}
func nativeFixture(t *testing.T, run *continuity.Run, messages []provider.Message) (string, *session.Session) {
	t.Helper()
	dir := t.TempDir()
	s := session.New(dir, run.Model)
	s.Title = "native test"
	s.SetRunState(run)
	s.SetMessages(append([]provider.Message{{Role: "system", Content: "fixture"}}, messages...))
	if err := s.SaveRecovery("error"); err != nil {
		t.Fatal(err)
	}
	loaded, err := session.Load(dir, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	return dir, loaded
}
func writeCall(id, path, value string) provider.ToolCall {
	args, _ := json.Marshal(map[string]string{"path": path, "content": value})
	return provider.ToolCall{ID: id, Type: "function", Function: provider.FunctionCall{Name: "write_file", Arguments: string(args)}}
}

func TestNativeErrorRecoverySurvivesRestart(t *testing.T) {
	root := resolvedTempDir(t)
	dir := t.TempDir()
	sess := session.New(dir, "mock/model")
	sess.Title = "native test"
	first := engine.New(nativeOpts(sess, root, nativeResumeBackend(func([]provider.Message) (provider.Message, error) {
		return provider.Message{}, errors.New("native stream broke")
	})))
	if err := first.RunTurn(context.Background(), "finish saved work"); err == nil {
		t.Fatal("expected stream failure")
	}
	_ = first.Close()
	loaded, err := session.Load(dir, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recoveryReason(t, loaded.RunState()) != "error" {
		t.Fatal("error snapshot has no explicit recovery provenance")
	}
	calls := 0
	second := engine.New(nativeOpts(loaded, root, nativeResumeBackend(func([]provider.Message) (provider.Message, error) {
		calls++
		onDisk, e := session.Load(dir, sess.ID)
		if e != nil {
			t.Fatal(e)
		}
		if recoveryReason(t, onDisk.RunState()) != "" || onDisk.RunState().LastPause != nil {
			t.Error("provider started before durable boundary retirement")
		}
		return provider.Message{Role: "assistant", Content: "finished"}, nil
	})))
	defer second.Close()
	input, ok := second.Resume()
	if !ok {
		t.Fatal("error snapshot cannot be claimed with /resume")
	}
	if err = second.RunTurn(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || loaded.RunState().Phase != "done" {
		t.Fatalf("calls=%d run=%+v", calls, loaded.RunState())
	}
}

func TestNativeResumeRunsOnlyUnansweredTools(t *testing.T) {
	root := resolvedTempDir(t)
	first, second := filepath.Join(root, "first"), filepath.Join(root, "second")
	if err := os.WriteFile(first, []byte("sentinel after completed work"), 0600); err != nil {
		t.Fatal(err)
	}
	run := nativeRun(t, root, "direct")
	messages := []provider.Message{{Role: "user", Content: run.Input}, {Role: "assistant", ToolCalls: []provider.ToolCall{writeCall("done", first, "WRONG REPLAY"), writeCall("pending", second, "new action")}}, {Role: "tool", ToolCallID: "done", Content: "wrote first"}}
	_, sess := nativeFixture(t, run, messages)
	calls := 0
	backend := nativeResumeBackend(func(messages []provider.Message) (provider.Message, error) {
		calls++
		seen := map[string]bool{}
		for _, m := range messages {
			if m.Role == "tool" {
				seen[m.ToolCallID] = true
				if strings.HasPrefix(m.Content, "Error:") {
					t.Errorf("tool failure: %s", m.Content)
				}
			}
		}
		if !seen["done"] || !seen["pending"] {
			t.Errorf("provider called before pending tools completed: %+v", messages)
		}
		return provider.Message{Role: "assistant", Content: "finished"}, nil
	})
	a := engine.New(nativeOpts(sess, root, backend))
	defer a.Close()
	input, ok := a.Resume()
	if !ok {
		t.Fatal("no native recovery claim")
	}
	if err := a.RunTurn(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(first); string(data) != "sentinel after completed work" {
		t.Fatalf("completed tool replayed: %q", data)
	}
	if data, err := os.ReadFile(second); string(data) != "new action" {
		t.Fatalf("pending tool not executed: %q err=%v messages=%+v", data, err, sess.GetMessages())
	}
	if calls != 1 || sess.RunState().Main.Rounds != 1 {
		t.Fatalf("calls=%d rounds=%d", calls, sess.RunState().Main.Rounds)
	}
}

func TestNativeCommittedReplyIsNotRegenerated(t *testing.T) {
	for _, phase := range []string{"direct", "synthesis"} {
		t.Run(phase, func(t *testing.T) {
			root := resolvedTempDir(t)
			run := nativeRun(t, root, phase)
			run.Main.SafeBoundary = "provider response committed"
			if phase == "synthesis" {
				run.Mode = engine.ModeAgent
				run.Tasks = []continuity.Task{{Title: "first", Model: "mock/model", Status: "done", State: continuity.TaskSettled, Result: "saved"}, {Title: "second", Model: "mock/model", Status: "done", State: continuity.TaskSettled, Result: "saved too"}}
			}
			_, sess := nativeFixture(t, run, []provider.Message{{Role: "user", Content: run.Input}, {Role: "assistant", Content: "already answered"}})
			calls := 0
			opts := nativeOpts(sess, root, nativeResumeBackend(func([]provider.Message) (provider.Message, error) {
				calls++
				return provider.Message{Role: "assistant", Content: "WRONG REPLAY"}, nil
			}))
			opts.Mode = run.Mode
			a := engine.New(opts)
			defer a.Close()
			input, ok := a.Resume()
			if !ok {
				t.Fatal("missing recovery claim")
			}
			if err := a.RunTurn(context.Background(), input); err != nil {
				t.Fatal(err)
			}
			if calls != 0 || sess.RunState().Phase != "done" {
				t.Fatalf("repeated answer: calls=%d run=%+v", calls, sess.RunState())
			}
		})
	}
}

type failRetirementSession struct {
	engine.SessionPort
	calls int
}

func (s *failRetirementSession) Save() error {
	s.calls++
	return errors.New("retirement disk unavailable")
}
func TestNativeResumeRequiresDurableRetirement(t *testing.T) {
	root := resolvedTempDir(t)
	run := nativeRun(t, root, "direct")
	target := filepath.Join(root, "must-not-exist")
	dir, sess := nativeFixture(t, run, []provider.Message{{Role: "user", Content: run.Input}, {Role: "assistant", ToolCalls: []provider.ToolCall{writeCall("pending", target, "unsafe")}}})
	store := &failRetirementSession{SessionPort: sess}
	calls := 0
	a := engine.New(nativeOpts(store, root, nativeResumeBackend(func([]provider.Message) (provider.Message, error) {
		calls++
		return provider.Message{Role: "assistant", Content: "unsafe"}, nil
	})))
	defer a.Close()
	input, ok := a.Resume()
	if !ok {
		t.Fatal("no recovery claim")
	}
	err := a.RunTurn(context.Background(), input)
	if err == nil || !strings.Contains(err.Error(), "retirement disk unavailable") || calls != 0 {
		t.Fatalf("retirement failure admitted work: %v calls=%d", err, calls)
	}
	if _, err = os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("tool ran despite failed retirement: %v", err)
	}
	if store.calls != 1 {
		t.Fatalf("failed retirement retried via ordinary JSON: %d writes", store.calls)
	}
	disk, err := session.Load(dir, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recoveryReason(t, disk.RunState()) != "error" {
		t.Fatal("failed retirement lost previous safe snapshot")
	}
	if _, ok := a.Resume(); !ok {
		t.Fatal("failed retirement lost in-memory retry claim")
	}
}

func TestNativeRecoveryClaimCanBeRetriedAfterUnstartedCancel(t *testing.T) {
	root := resolvedTempDir(t)
	run := nativeRun(t, root, "direct")
	_, sess := nativeFixture(t, run, []provider.Message{{Role: "user", Content: run.Input}})
	a := engine.New(nativeOpts(sess, root, nativeResumeBackend(func([]provider.Message) (provider.Message, error) {
		t.Fatal("cancel sent request")
		return provider.Message{}, nil
	})))
	defer a.Close()
	input, ok := a.Resume()
	if !ok {
		t.Fatal("missing claim")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := a.RunTurn(ctx, input); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, ok = a.Resume(); !ok {
		t.Fatal("unstarted cancellation consumed recovery claim")
	}
}

func TestNativeTaskRecoveryKeepsSettledResults(t *testing.T) {
	root := resolvedTempDir(t)
	run := nativeRun(t, root, "tasks")
	run.Mode = engine.ModeAgent
	run.Main.SafeBoundary = "provider response committed"
	run.Tasks = []continuity.Task{
		{Title: "finished", Model: "mock/model", Effort: engine.EffortMedium, State: continuity.TaskSettled, Status: "done", Result: "saved first result"},
		{Title: "failed", Model: "mock/model", Effort: engine.EffortMedium, State: continuity.TaskSettled, Status: "failed", Reason: "saved failure"},
		{Title: "queued", Kind: "explain", Model: "mock/model", Effort: engine.EffortMedium, State: continuity.TaskQueued, Needs: []int{0}},
	}
	dir, sess := nativeFixture(t, run, []provider.Message{{Role: "user", Content: run.Input}})
	calls, opened := 0, 0
	opts := nativeOpts(sess, root, nativeResumeBackend(func(messages []provider.Message) (provider.Message, error) {
		calls++
		data, _ := json.Marshal(messages)
		if !strings.Contains(string(data), "saved first result") || !strings.Contains(string(data), "saved failure") {
			t.Errorf("synthesis lost saved outcomes: %s", data)
		}
		return provider.Message{Role: "assistant", Content: "all results"}, nil
	}))
	opts.Mode = engine.ModeAgent
	opts.SubagentBackend = func(context.Context, string, string, string, engine.SubagentCapabilities) (engine.ChatBackend, error) {
		opened++
		disk, err := session.Load(dir, sess.ID)
		if err != nil {
			t.Fatal(err)
		}
		if recoveryReason(t, disk.RunState()) != "" {
			t.Error("child opened before recovery retired")
		}
		return nativeResumeBackend(func([]provider.Message) (provider.Message, error) {
			return provider.Message{Role: "assistant", Content: "third result"}, nil
		}), nil
	}
	a := engine.New(opts)
	defer a.Close()
	input, ok := a.Resume()
	if !ok {
		t.Fatal("missing claim")
	}
	if err := a.RunTurn(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || opened != 1 {
		t.Fatalf("replanned/repeated settled tasks: main calls=%d opened=%d", calls, opened)
	}
	saved := sess.RunState()
	if saved.Tasks[0].Result != "saved first result" || saved.Tasks[1].Status != "failed" || saved.Tasks[2].Result != "third result" {
		t.Fatalf("changed settled outcomes: %+v", saved.Tasks)
	}
}

type liveRetirementSession struct {
	*session.Session
	captured bool
	fail     bool
	saved    int
}

func (s *liveRetirementSession) SaveRecovery(reason string) error {
	err := s.Session.SaveRecovery(reason)
	if err == nil {
		s.captured = true
	}
	return err
}
func (s *liveRetirementSession) Save() error {
	if s.captured {
		s.saved++
		if s.fail {
			return errors.New("live retirement failed")
		}
	}
	return s.Session.Save()
}
func TestNativeLiveRecoveryRetiresBeforeTheNextTool(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "saved", true: "failed"}[fail], func(t *testing.T) {
			root := resolvedTempDir(t)
			dir := t.TempDir()
			sess := &liveRetirementSession{Session: session.New(dir, "mock/model"), fail: fail}
			sess.Title = "live recovery"
			target := filepath.Join(root, "next")
			calls := 0
			backend := nativeResumeBackend(func([]provider.Message) (provider.Message, error) {
				calls++
				if calls == 1 {
					return provider.Message{Role: "assistant", ToolCalls: []provider.ToolCall{{ID: "bad", Function: provider.FunctionCall{Name: "read_file", Arguments: `{"path":"missing-file"}`}}, writeCall("next", target, "one action")}}, nil
				}
				return provider.Message{Role: "assistant", Content: "done"}, nil
			})
			opts := nativeOpts(sess, root, backend)
			opts.PostWrite = func(_, _ string) {
				disk, err := session.Load(dir, sess.ID)
				if err != nil {
					t.Error(err)
					return
				}
				if recoveryReason(t, disk.RunState()) != "" {
					t.Error("write happened while old recovery still eligible")
				}
			}
			a := engine.New(opts)
			defer a.Close()
			err := a.RunTurn(context.Background(), "recover then continue")
			if !sess.captured {
				t.Fatal("native error did not capture a boundary")
			}
			if fail {
				if err == nil || !strings.Contains(err.Error(), "live retirement failed") || calls != 1 {
					t.Fatalf("retirement failure continued: %v calls=%d", err, calls)
				}
				if _, err = os.Stat(target); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("tool admitted: %v", err)
				}
				if sess.saved != 1 {
					t.Errorf("failed retirement retried %d times", sess.saved)
				}
			} else {
				if err != nil || calls != 2 {
					t.Fatalf("continuation failed: %v calls=%d", err, calls)
				}
				if data, _ := os.ReadFile(target); string(data) != "one action" {
					t.Errorf("missing successful next tool: %q", data)
				}
			}
		})
	}
}

func TestNativeHardExitAndUnknownBoundaryRefuseWithoutWork(t *testing.T) {
	for _, reason := range []string{"", "invented"} {
		t.Run("reason="+reason, func(t *testing.T) {
			root := resolvedTempDir(t)
			run := nativeRun(t, root, "direct")
			data, _ := json.Marshal(map[string]string{"recovery": reason})
			_ = json.Unmarshal(data, run)
			_, sess := nativeFixture(t, run, []provider.Message{{Role: "user", Content: run.Input}})
			calls := 0
			a := engine.New(nativeOpts(sess, root, nativeResumeBackend(func([]provider.Message) (provider.Message, error) { calls++; return provider.Message{}, nil })))
			defer a.Close()
			err := a.RunTurn(context.Background(), run.Input)
			if err == nil || calls != 0 {
				t.Fatalf("uncertain record admitted work: %v calls=%d", err, calls)
			}
			if sess.RunState().ID != run.ID || sess.RunState().Phase != run.Phase {
				t.Fatal("refusal discarded journal")
			}
		})
	}
}

type nativeVendorFailure struct{ calls int }

func (b *nativeVendorFailure) ProviderHandle() string { return "" }
func (b *nativeVendorFailure) StreamChat(context.Context, string, []provider.Message, []provider.Tool, func(string)) (provider.Message, provider.Meta, error) {
	b.calls++
	return provider.Message{}, provider.Meta{}, errors.New("vendor failed before its handle arrived")
}
func TestNativeRecoveryDoesNotReplayAVendorWithNoHandle(t *testing.T) {
	root := resolvedTempDir(t)
	dir := t.TempDir()
	sess := session.New(dir, "mock/model")
	sess.Title = "vendor test"
	backend := &nativeVendorFailure{}
	a := engine.New(nativeOpts(sess, root, backend))
	if err := a.RunTurn(context.Background(), "vendor work"); err == nil {
		t.Fatal("missing error")
	}
	_ = a.Close()
	loaded, err := session.Load(dir, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.RunState().Main.ProviderOwned {
		t.Fatal("empty-handle vendor ownership was lost")
	}
	b := engine.New(nativeOpts(loaded, root, backend))
	defer b.Close()
	input, ok := b.Resume()
	if !ok {
		t.Fatal("missing saved work")
	}
	err = b.RunTurn(context.Background(), input)
	if err == nil || !strings.Contains(err.Error(), "vendor") || backend.calls != 1 {
		t.Fatalf("accepted unfinished vendor prompt was resent: %v calls=%d", err, backend.calls)
	}
	if _, ok = b.Resume(); !ok {
		t.Fatal("refusal lost claim")
	}
}

func TestNativeDiscardDoesNotExecuteOldPendingCalls(t *testing.T) {
	root := resolvedTempDir(t)
	target := filepath.Join(root, "abandoned")
	run := nativeRun(t, root, "direct")
	_, sess := nativeFixture(t, run, []provider.Message{{Role: "user", Content: run.Input}, {Role: "assistant", ToolCalls: []provider.ToolCall{writeCall("old", target, "wrong")}}})
	calls := 0
	a := engine.New(nativeOpts(sess, root, nativeResumeBackend(func([]provider.Message) (provider.Message, error) {
		calls++
		return provider.Message{Role: "assistant", Content: "new work"}, nil
	})))
	defer a.Close()
	if !a.DiscardPending() {
		t.Fatal("discard failed")
	}
	if err := a.RunTurn(context.Background(), "a different request"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal(calls)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("executed abandoned call: %v", err)
	}
}

type failedNativeRecovery struct{ *session.Session }

func (s *failedNativeRecovery) SaveRecovery(string) error {
	return errors.New("snapshot volume unavailable")
}
func TestNativeFailedSnapshotCannotGainRecoveryProvenance(t *testing.T) {
	root := resolvedTempDir(t)
	dir := t.TempDir()
	sess := &failedNativeRecovery{Session: session.New(dir, "mock/model")}
	sess.Title = "failed snapshot"
	a := engine.New(nativeOpts(sess, root, nativeResumeBackend(func([]provider.Message) (provider.Message, error) {
		return provider.Message{}, errors.New("native failed")
	})))
	defer a.Close()
	if err := a.RunTurn(context.Background(), "native work"); err == nil {
		t.Fatal("expected failure")
	}
	if recoveryReason(t, sess.RunState()) != "" {
		t.Fatal("failed save left a recovery marker in memory")
	}
	if err := sess.Save(); err != nil {
		t.Fatal(err)
	}
	disk, err := session.Load(dir, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	b := engine.New(nativeOpts(disk, root, nativeResumeBackend(func([]provider.Message) (provider.Message, error) {
		t.Fatal("unsaved boundary replayed")
		return provider.Message{}, nil
	})))
	defer b.Close()
	if _, ok := b.Resume(); ok {
		t.Fatal("later JSON save claimed failed snapshot as recoverable")
	}
}

func TestNativeRecoveryCannotSwitchToAnOwnedBackend(t *testing.T) {
	root := resolvedTempDir(t)
	run := nativeRun(t, root, "direct")
	_, sess := nativeFixture(t, run, []provider.Message{{Role: "user", Content: run.Input}})
	backend := &nativeVendorFailure{}
	a := engine.New(nativeOpts(sess, root, backend))
	defer a.Close()
	input, ok := a.Resume()
	if !ok {
		t.Fatal("missing claim")
	}
	err := a.RunTurn(context.Background(), input)
	if err == nil || !strings.Contains(err.Error(), "vendor-owned") || backend.calls != 0 {
		t.Fatalf("native snapshot moved to vendor: %v calls=%d", err, backend.calls)
	}
}

func TestNativeLiveChildRecoveryRetiresBeforeAdmission(t *testing.T) {
	root := resolvedTempDir(t)
	dir := t.TempDir()
	sess := session.New(dir, "mock/model")
	sess.Title = "live children"
	mainCalls, opened := 0, 0
	opts := nativeOpts(sess, root, nativeResumeBackend(func([]provider.Message) (provider.Message, error) {
		mainCalls++
		if mainCalls == 1 {
			return provider.Message{Role: "assistant", Content: `[{"title":"first","kind":"explain"},{"title":"second","kind":"explain"}]`}, nil
		}
		return provider.Message{Role: "assistant", Content: "partial results"}, nil
	}))
	opts.Mode = engine.ModeAgent
	opts.MaxConcurrentTasks = 1
	opts.SubagentBackend = func(context.Context, string, string, string, engine.SubagentCapabilities) (engine.ChatBackend, error) {
		opened++
		if opened == 1 {
			return nativeResumeBackend(func([]provider.Message) (provider.Message, error) {
				return provider.Message{}, errors.New("first child failed")
			}), nil
		}
		disk, err := session.Load(dir, sess.ID)
		if err != nil {
			return nil, err
		}
		if recoveryReason(t, disk.RunState()) != "" {
			t.Error("queued child opened before live error recovery retired")
		}
		return nativeResumeBackend(func([]provider.Message) (provider.Message, error) {
			return provider.Message{Role: "assistant", Content: "second done"}, nil
		}), nil
	}
	a := engine.New(opts)
	defer a.Close()
	if err := a.RunTurn(context.Background(), "two things"); err != nil {
		t.Fatal(err)
	}
	if mainCalls != 2 || opened != 2 {
		t.Fatalf("main=%d children=%d", mainCalls, opened)
	}
}
