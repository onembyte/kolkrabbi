package engine_test

// Adopted from the V43.5 review (EG1): the saved working-directory binding is
// enforced on resume, and a refused resume loses nothing. Removing the check
// in execution.go survived every other suite.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

func TestAResumeIsBoundToTheProjectThatStartedIt(t *testing.T) {
	rootA, _ := filepath.EvalSymlinks(t.TempDir())
	rootB, _ := filepath.EvalSymlinks(t.TempDir())
	path := filepath.Join(rootA, "a.txt")
	args, _ := json.Marshal(map[string]string{"path": path, "content": "once"})
	srv := enginetest.New(
		enginetest.Step{Text: `[{"title":"write","kind":"edit"},{"title":"review","kind":"explain","needs":[1]}]`},
		enginetest.Step{ToolCalls: []provider.ToolCall{{ID: "w", Type: "function", Function: provider.FunctionCall{Name: "write_file", Arguments: string(args)}}}},
		enginetest.Step{StatusCode: http.StatusTooManyRequests, RetryAfter: "1800", ErrorBody: `{"error":{"message":"rate limited"}}`},
		enginetest.Step{Text: "written"},
		enginetest.Step{Text: "reviewed"},
		enginetest.Step{Text: "both done"},
	)
	defer srv.Close()
	dir := t.TempDir()
	sess := session.New(dir, "mock/model")
	var out bytes.Buffer
	opts := engine.Options{Client: provider.NewCompatibleClient(srv.URL), Sess: sess, Root: rootA, Model: "mock/model",
		Mode: engine.ModeAgent, Permission: engine.PermissionFullAuto, Out: &out, MaxConcurrentTasks: 1}
	a := engine.New(opts)
	const input = "write then review"
	var paused *engine.PausedError
	if err := a.RunTurn(context.Background(), input); !errors.As(err, &paused) {
		t.Fatalf("want pause: %v", err)
	}
	_ = a.Close()
	loaded, _ := session.Load(dir, sess.ID)

	// Another project opens the same session.
	opts.Sess, opts.Root = loaded, rootB
	b := engine.New(opts)
	pending, ok := b.Resume()
	if !ok {
		t.Fatal("no claim")
	}
	err := b.RunTurn(context.Background(), pending)
	t.Logf("resume in another root: %v", err)
	if err == nil || !strings.Contains(err.Error(), rootA) {
		t.Errorf("binding not enforced: %v", err)
	}
	if again, ok := b.Resume(); !ok || again != input {
		t.Errorf("refused resume lost the claim: %q %v", again, ok)
	}
	_ = b.Close()
	// The owning project can still resume it without repeating the write.
	if err := os.WriteFile(path, []byte("sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	opts.Root = rootA
	c := engine.New(opts)
	defer c.Close()
	pending, ok = c.Resume()
	if !ok {
		t.Fatal("owning project lost the pause")
	}
	if err := c.RunTurn(context.Background(), pending); err != nil {
		t.Fatalf("owning project resume: %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "sentinel" {
		t.Errorf("completed write repeated: %q", got)
	}
	if loaded.RunState().Phase != "done" {
		t.Errorf("phase %s", loaded.RunState().Phase)
	}
}
