package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/session"
)

// Reload the real durable session, not the previous Agent or a shared fake.
// A sentinel written after the pause detects a replayed filesystem action.
func TestAllowanceResumeAfterRestartDoesNotRepeatTools(t *testing.T) {
	for _, mode := range []string{engine.ModeCode, engine.ModeAgent, "agent-direct"} {
		t.Run(mode, func(t *testing.T) {
			root := resolvedTempDir(t)
			path := filepath.Join(root, "result.txt")
			args, _ := json.Marshal(map[string]string{"path": path, "content": "first action"})
			var steps []enginetest.Step
			actualMode := mode
			switch mode {
			case engine.ModeAgent:
				steps = append(steps, enginetest.Step{Text: `[{"title":"write the result","kind":"edit"},{"title":"review result","kind":"explain","needs":[1]}]`})
			case "agent-direct":
				actualMode = engine.ModeAgent
				steps = append(steps, enginetest.Step{Text: `[{"title":"write the result","kind":"edit"}]`})
			}
			steps = append(steps,
				enginetest.Step{ToolCalls: []provider.ToolCall{{ID: "write-once", Type: "function", Function: provider.FunctionCall{Name: "write_file", Arguments: string(args)}}}},
				enginetest.Step{StatusCode: http.StatusTooManyRequests, RetryAfter: "1800", ErrorBody: `{"error":{"message":"rate limited"}}`},
				enginetest.Step{Text: "the file was written"},
			)
			if mode == engine.ModeAgent {
				steps = append(steps, enginetest.Step{Text: "review complete"}, enginetest.Step{Text: "both tasks complete"})
			}
			srv := enginetest.New(steps...)
			defer srv.Close()
			dir := t.TempDir()
			sess := session.New(dir, "mock/model")
			opts := engine.Options{Client: provider.NewCompatibleClient(srv.URL), Sess: sess, Root: root,
				Model: "mock/model", Mode: actualMode, Permission: engine.PermissionFullAuto, Out: io.Discard, MaxConcurrentTasks: 1}
			a := engine.New(opts)
			var paused *engine.PausedError
			const input = "write a file and report the result"
			if err := a.RunTurn(context.Background(), input); !errors.As(err, &paused) {
				t.Fatalf("expected pause, got %v", err)
			}
			if content, err := os.ReadFile(path); err != nil || string(content) != "first action" {
				t.Fatalf("first tool did not run: %q, %v", content, err)
			}
			// Exercise the accepted-but-not-started delivery window too.
			if pending, ok := a.Resume(); !ok || pending != input {
				t.Fatalf("resume claim = %q, %v", pending, ok)
			}
			_ = a.Close()
			if err := os.WriteFile(path, []byte("sentinel after pause"), 0o600); err != nil {
				t.Fatal(err)
			}
			loaded, err := session.Load(dir, sess.SessionID())
			if err != nil {
				t.Fatal(err)
			}
			opts.Sess = loaded
			b := engine.New(opts)
			defer b.Close()
			pending, ok := b.Resume()
			if !ok || pending != input {
				t.Fatalf("saved journal lost the claimed request: %q, %v", pending, ok)
			}
			if err := b.RunTurn(context.Background(), pending); err != nil {
				t.Fatalf("resume: %v", err)
			}
			if len(srv.Requests) != len(steps) {
				t.Fatalf("got %d requests, want %d", len(srv.Requests), len(steps))
			}
			resumedRequest := srv.Requests[len(steps)-1]
			if mode == engine.ModeAgent {
				resumedRequest = srv.Requests[3]
			}
			found := false
			for _, msg := range resumedRequest {
				found = found || msg.Role == "tool" && msg.ToolCallID == "write-once"
			}
			if !found {
				t.Fatal("resumed provider request lost the existing tool result")
			}
			if content, _ := os.ReadFile(path); string(content) != "sentinel after pause" {
				t.Fatalf("resumption repeated a completed write: %q", content)
			}
			users := 0
			for _, msg := range loaded.GetMessages() {
				if msg.Role == "user" && strings.Contains(msg.Content, input) {
					users++
				}
			}
			if users != 1 || loaded.RunState().Phase != "done" {
				t.Fatalf("users=%d, phase=%s", users, loaded.RunState().Phase)
			}
		})
	}
}
