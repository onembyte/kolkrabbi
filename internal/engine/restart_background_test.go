package engine_test

import (
	"context"
	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/session"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// At a quiescent boundary a settled background child, interrupted child and
// dependent queued child must survive together in the real compressed store.
func TestIndependentRestartMixedBackgroundWorkKeepsSettledActionsAndDependencies(t *testing.T) {
	root := resolvedTempDir(t)
	path := filepath.Join(root, "already-written")
	if err := os.WriteFile(path, []byte("sentinel after completed write"), 0600); err != nil {
		t.Fatal(err)
	}
	run := nativeRun(t, root, "tasks")
	run.Mode = engine.ModeAgent
	run.Main.State = continuity.TaskWaiting
	run.Main.SafeBoundary = "provider response committed"
	run.Tasks = []continuity.Task{
		{Title: "settled background", Kind: "explain", Model: "mock/settled", Effort: engine.EffortMedium, State: continuity.TaskSettled, Status: "done", Result: "settled child result"},
		{Title: "interrupted background", Kind: "explain", Model: "mock/waiting", Effort: engine.EffortMedium, State: continuity.TaskWaiting, Rounds: 1, ProviderInFlight: true, SafeBoundary: "tool results committed", Messages: []provider.Message{
			{Role: "system", Content: "saved child instructions"}, {Role: "user", Content: "finish child"},
			{Role: "assistant", ToolCalls: []provider.ToolCall{writeCall("saved-write", path, "WRONG: replay")}},
			{Role: "tool", ToolCallID: "saved-write", Content: "completed write"},
		}},
		{Title: "queued dependent", Kind: "explain", Model: "mock/queued", Effort: engine.EffortMedium, State: continuity.TaskQueued, Needs: []int{0, 1}},
	}
	dir, sess := nativeFixture(t, run, []provider.Message{{Role: "user", Content: run.Input}})
	var mu sync.Mutex
	opened := map[string]int{}
	synthesized := 0
	opts := nativeOpts(sess, root, nativeResumeBackend(func(messages []provider.Message) (provider.Message, error) {
		synthesized++
		var joined strings.Builder
		for _, m := range messages {
			joined.WriteString(m.Content)
		}
		for _, result := range []string{"settled child result", "resumed child result", "queued child result"} {
			if !strings.Contains(joined.String(), result) {
				t.Errorf("synthesis lost %q", result)
			}
		}
		return provider.Message{Role: "assistant", Content: "complete"}, nil
	}))
	opts.Mode = engine.ModeAgent
	opts.MaxConcurrentTasks = 2
	opts.SubagentBackend = func(_ context.Context, model, _, _ string, _ engine.SubagentCapabilities) (engine.ChatBackend, error) {
		mu.Lock()
		opened[model]++
		mu.Unlock()
		disk, err := session.Load(dir, sess.ID)
		if err != nil {
			return nil, err
		}
		if disk.RunState().Recovery != "" {
			t.Error("background work opened before durable retirement")
		}
		return nativeResumeBackend(func(messages []provider.Message) (provider.Message, error) {
			switch model {
			case "mock/waiting":
				found := false
				for _, m := range messages {
					found = found || m.Role == "tool" && m.ToolCallID == "saved-write"
				}
				if !found {
					t.Error("background continuation lost completed tool result")
				}
				return provider.Message{Role: "assistant", Content: "resumed child result"}, nil
			case "mock/queued":
				var joined strings.Builder
				for _, m := range messages {
					joined.WriteString(m.Content)
				}
				if !strings.Contains(joined.String(), "settled child result") || !strings.Contains(joined.String(), "resumed child result") {
					t.Error("queued child started without saved/resumed dependency results")
				}
				return provider.Message{Role: "assistant", Content: "queued child result"}, nil
			default:
				t.Errorf("settled background child reopened: %s", model)
				return provider.Message{Role: "assistant", Content: "wrong"}, nil
			}
		}), nil
	}
	ag := engine.New(opts)
	defer ag.Close()
	pending, ok := ag.Resume()
	if !ok {
		t.Fatal("missing restart claim")
	}
	if err := ag.RunTurn(context.Background(), pending); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "sentinel after completed write" {
		t.Fatalf("completed action replayed: %q %v", data, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if synthesized != 1 || len(opened) != 2 || opened["mock/waiting"] != 1 || opened["mock/queued"] != 1 {
		t.Fatalf("opened=%v synthesis=%d", opened, synthesized)
	}
	if saved := sess.RunState(); saved.Phase != "done" || saved.Tasks[0].Result != "settled child result" {
		t.Fatalf("saved work lost: %+v", saved)
	}
}
