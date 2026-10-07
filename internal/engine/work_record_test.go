package engine

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/tools"
)

func TestWorkLogReportsCompletedChangesAndRealCommandFailure(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("before\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var records []WorkRecord
	a := &Agent{Options: Options{Root: root, Out: io.Discard, WorkLog: func(r WorkRecord) { records = append(records, r) }}}
	guard := func(context.Context, io.Writer) tools.Guard { return nil }
	ctx := context.WithValue(context.Background(), workAgentKey{}, 2)
	for _, call := range []provider.ToolCall{
		{Function: provider.FunctionCall{Name: "edit_file", Arguments: fmt.Sprintf(`{"path":%q,"old_str":"before","new_str":"after","purpose":"clarify the message"}`, filepath.Join(root, "a.txt"))}},
		{Function: provider.FunctionCall{Name: "bash", Arguments: `{"command":"printf 'failed check'; exit 7"}`}},
	} {
		_, _ = a.executeToolWith(ctx, call, io.Discard, EffortLow, guard, false, root)
	}
	if len(records) != 2 {
		t.Fatalf("records=%+v", records)
	}
	if r := records[0]; r.Agent != 2 || !r.Changed || r.Failed || r.Added != 1 || r.Removed != 1 || !strings.Contains(r.Diff, "+after") {
		t.Fatalf("edit=%+v", r)
	}
	if r := records[1]; !r.Failed || !strings.Contains(r.Output, "failed check") || !strings.Contains(r.Output, "exit error") {
		t.Fatalf("command=%+v", r)
	}
	deny := func(context.Context, io.Writer) tools.Guard { return func(tools.Request) bool { return false } }
	_, _ = a.executeToolWith(ctx, provider.ToolCall{Function: provider.FunctionCall{Name: "write_file", Arguments: fmt.Sprintf(`{"path":%q,"content":"denied"}`, filepath.Join(root, "a.txt"))}}, io.Discard, EffortLow, deny, false, root)
	if r := records[2]; !r.Failed || r.Changed || r.Diff != "" {
		t.Fatalf("denied edit claimed a change: %+v", r)
	}
	body, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(body) != "after\n" {
		t.Fatal("denied write changed the file")
	}
}

func TestWorkLogCorrelatesConcurrentProviderToolsPerAgent(t *testing.T) {
	var mu sync.Mutex
	var records []WorkRecord
	a := &Agent{Options: Options{WorkLog: func(r WorkRecord) { mu.Lock(); defer mu.Unlock(); records = append(records, r) }}}
	var wg sync.WaitGroup
	for _, index := range []int{1, 2, 3} {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			observe := a.providerWorkLog(index)
			observe(provider.ProgressEvent{Kind: provider.ProgressToolStarted, ID: "same-id", Name: "Bash", Input: `{"command":"go test ./..."}`})
			observe(provider.ProgressEvent{Kind: provider.ProgressToolFinished, ID: "same-id", Output: "line one\nline two"})
			observe(provider.ProgressEvent{Kind: provider.ProgressToolFinished, ID: "same-id", Output: "duplicate"})
		}(index)
	}
	wg.Wait()
	if len(records) != 3 {
		t.Fatalf("records=%+v", records)
	}
	seen := map[int]bool{}
	for _, r := range records {
		seen[r.Agent] = true
		if r.Name != "Bash" || !strings.Contains(r.Output, "\n") || r.Arguments == "" {
			t.Fatal(r)
		}
	}
	if len(seen) != 3 {
		t.Fatal("crossed agent ownership")
	}
}

func TestWorkLogPreservesWarningsAndUnfinishedProviderTools(t *testing.T) {
	var records []WorkRecord
	a := &Agent{Options: Options{Model: "mock/model", WorkLog: func(r WorkRecord) { records = append(records, r) },
		Backend: observedWorkBackend{events: []provider.ProgressEvent{
			{Kind: provider.ProgressToolStarted, ID: "pending", Name: "Bash", Input: `{"command":"go test ./..."}`},
			{Kind: provider.ProgressWarning, Name: "codex", Input: "model metadata unavailable"},
		}}}}
	if _, _, err := a.streamChatObserved(context.Background(), activityWorking, "mock/model", nil, nil, nil, a.mainProviderProgress("mock/model", EffortHigh)); err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || !records[0].Warning || !strings.Contains(records[0].Output, "metadata unavailable") {
		t.Fatalf("warning vanished: %+v", records)
	}
	if r := records[1]; !r.Pending || r.Failed || !strings.Contains(r.Arguments, "go test") || r.Output != "No completion reported" {
		t.Fatalf("unfinished tool vanished or claimed success: %+v", r)
	}
}
