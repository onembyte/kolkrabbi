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

func TestChildCompactionThenPauseRetainsHistoryAcrossRestart(t *testing.T) {
	root := resolvedTempDir(t)
	path := filepath.Join(root, "evidence.txt")
	const marker = "MIDDLE_EVIDENCE_RETAINED_ONLY_IN_ARCHIVE"
	if err := os.WriteFile(path, []byte(strings.Repeat("first line\n", 200)+marker+strings.Repeat("last line\n", 200)), 0o600); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{"path": path})
	srv := enginetest.New(
		enginetest.Step{Text: `[{"title":"inspect evidence","kind":"research"},{"title":"verify result","kind":"research","needs":[1]}]`},
		enginetest.Step{ToolCalls: []provider.ToolCall{{ID: "read-once", Type: "function", Function: provider.FunctionCall{Name: "read_file", Arguments: string(args)}}}},
		enginetest.Step{StatusCode: http.StatusBadRequest, ErrorBody: `{"error":{"message":"context_length_exceeded"}}`},
		enginetest.Step{StatusCode: http.StatusTooManyRequests, RetryAfter: "1800", ErrorBody: `{"error":{"message":"rate limited"}}`},
		enginetest.Step{Text: "inspection complete"},
		enginetest.Step{Text: "verification complete"},
		enginetest.Step{Text: "both complete"},
	)
	defer srv.Close()
	dir := t.TempDir()
	sess := session.New(dir, "mock/model")
	opts := engine.Options{Client: provider.NewCompatibleClient(srv.URL), Sess: sess, Root: root,
		Model: "mock/model", Mode: engine.ModeAgent, Permission: engine.PermissionFullAuto, Out: io.Discard, MaxConcurrentTasks: 1}
	a := engine.New(opts)
	var paused *engine.PausedError
	if err := a.RunTurn(context.Background(), "inspect then verify the evidence"); !errors.As(err, &paused) {
		t.Fatalf("expected pause after context recovery: %v", err)
	}
	_ = a.Close()
	loaded, err := session.Load(dir, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	child := loaded.RunState().Tasks[0]
	if len(child.Archives) != 1 || child.Rounds != 1 || len(child.Messages) != 4 {
		t.Fatalf("compaction lost child state: %+v", child)
	}
	if strings.Contains(child.Messages[3].Content, marker) {
		t.Fatal("fixture did not actually shorten the tool output")
	}
	archives, err := loaded.CompactionHistory()
	if err != nil || len(archives) != 1 || !strings.Contains(archives[0].Messages[3].Content, marker) {
		t.Fatalf("full child history did not survive restart: %v", err)
	}
	opts.Sess = loaded
	b := engine.New(opts)
	defer b.Close()
	pending, ok := b.Resume()
	if !ok {
		t.Fatal("compacted child lost its pause")
	}
	if err := b.RunTurn(context.Background(), pending); err != nil {
		t.Fatal(err)
	}
	if len(srv.Requests) != 7 || loaded.RunState().Phase != "done" {
		t.Fatalf("resumption repeated work: %d requests", len(srv.Requests))
	}
}
