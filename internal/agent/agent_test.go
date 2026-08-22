package agent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kolkrabbi/internal/api"
	"kolkrabbi/internal/checkpoint"
	"kolkrabbi/internal/mockrouter"
	"kolkrabbi/internal/session"
	"kolkrabbi/internal/stats"
)

func newTestAgent(t *testing.T, srv *mockrouter.Server, mode string) (*Agent, *bytes.Buffer, string, string) {
	t.Helper()
	client := api.NewClient("test-key")
	client.BaseURL = srv.URL
	sdir, statsDir := t.TempDir(), t.TempDir()
	sess := session.New(sdir, "mock/model")
	ckpt, err := checkpoint.Open(sess.CkptDir())
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	ag := New(Options{
		Client: client, Model: "mock/model", Mode: mode, Yolo: true,
		Sess: sess, Ckpt: ckpt, Out: &out, StatsDir: statsDir,
	})
	return ag, &out, sdir, statsDir
}

// TestE2E_ToolLoopWithPersistenceAndRewind exercises the whole code-mode
// pipeline offline: scripted model responses -> streaming/accumulation ->
// tool execution on disk -> results fed back -> final answer -> session
// saved -> stats recorded -> checkpoint rewind restores the filesystem.
func TestE2E_ToolLoopWithPersistenceAndRewind(t *testing.T) {
	work := t.TempDir()
	target := filepath.Join(work, "hello.txt")

	srv := mockrouter.New(
		mockrouter.Step{
			Text: "Creating the file now.",
			ToolCalls: []api.ToolCall{{
				ID: "call_1",
				Function: api.FunctionCall{
					Name:      "write_file",
					Arguments: `{"path":"` + jsonEsc(target) + `","content":"hi from kolk\n"}`,
				},
			}},
		},
		mockrouter.Step{Text: "Done — hello.txt is created.", Cost: 0.002},
	)
	defer srv.Close()

	ag, out, sdir, statsDir := newTestAgent(t, srv, ModeCode)
	if err := ag.RunTurn(context.Background(), "create a hello file"); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	// 1. the tool actually ran
	b, err := os.ReadFile(target)
	if err != nil || string(b) != "hi from kolk\n" {
		t.Fatalf("file content = %q, err=%v", string(b), err)
	}

	// 2. streamed output + cost footer reached the writer
	if !strings.Contains(out.String(), "Creating the file now.") || !strings.Contains(out.String(), "Done — hello.txt is created.") {
		t.Errorf("streamed output missing content:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "$0.0020") {
		t.Errorf("footer missing cost:\n%s", out.String())
	}

	// 3. the second request carried the tool result back to the model
	if len(srv.Requests) != 2 {
		t.Fatalf("mock server got %d requests, want 2", len(srv.Requests))
	}
	foundResult := false
	for _, m := range srv.Requests[1] {
		if m.Role == "tool" && m.ToolCallID == "call_1" && strings.Contains(m.Content, "wrote") {
			foundResult = true
		}
	}
	if !foundResult {
		t.Errorf("tool result was not sent back to the model")
	}

	// 4. session persisted with the full history
	loaded, err := session.Load(sdir, ag.Sess.ID)
	if err != nil {
		t.Fatalf("session was not saved: %v", err)
	}
	if len(loaded.Messages) != 5 {
		t.Fatalf("persisted %d messages, want 5", len(loaded.Messages))
	}

	// 5. both calls were recorded locally with usage
	recs, err := stats.Load(statsDir)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	for _, r := range recs {
		if r.Kind == "call" {
			calls++
			if r.Model != "mock/model" || r.PromptTokens == 0 {
				t.Errorf("bad stats record: %+v", r)
			}
		}
	}
	if calls != 2 {
		t.Errorf("recorded %d calls, want 2", calls)
	}

	// 6. rating the turn lands in stats and aggregates
	if err := ag.RateLast(5); err != nil {
		t.Fatalf("RateLast: %v", err)
	}
	recs, _ = stats.Load(statsDir)
	rows := stats.Aggregate(recs)
	if len(rows) != 1 || rows[0].AvgRating != 5 || rows[0].Ratings == 0 {
		t.Errorf("aggregate rows = %+v, want mock/model rated 5", rows)
	}

	// 7. rewind restores the filesystem (created file is removed)
	restored, err := ag.Rewind()
	if err != nil || len(restored) != 1 {
		t.Fatalf("rewind = (%v,%v), want 1 path", restored, err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("file still exists after rewind")
	}
}

// TestE2E_ChatModeHasNoTools verifies chat mode sends no tool schemas and
// answers plainly.
func TestE2E_ChatModeHasNoTools(t *testing.T) {
	srv := mockrouter.New(mockrouter.Step{Text: "hola Franco"})
	defer srv.Close()

	ag, out, _, _ := newTestAgent(t, srv, ModeChat)
	if err := ag.RunTurn(context.Background(), "say hi"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "hola Franco") {
		t.Errorf("missing reply: %s", out.String())
	}
	if got := srv.Tools[0]; got != 0 {
		t.Errorf("chat mode sent %d tools, want 0", got)
	}
}

// TestE2E_OrchestratedAgentMode drives the full agent-mode pipeline: plan ->
// two subagents (one uses a tool) -> synthesis, with isolated contexts and a
// compact main history.
func TestE2E_OrchestratedAgentMode(t *testing.T) {
	work := t.TempDir()
	target := filepath.Join(work, "notes.txt")

	srv := mockrouter.New(
		// planner
		mockrouter.Step{Text: `["write the notes file", "verify it exists"]`},
		// subagent 1: tool call then summary
		mockrouter.Step{ToolCalls: []api.ToolCall{{
			ID: "c1",
			Function: api.FunctionCall{
				Name:      "write_file",
				Arguments: `{"path":"` + jsonEsc(target) + `","content":"orchestrated\n"}`,
			},
		}}},
		mockrouter.Step{Text: "Wrote notes.txt with the content."},
		// subagent 2: plain summary
		mockrouter.Step{Text: "Verified: notes.txt exists."},
		// synthesis
		mockrouter.Step{Text: "Both tasks complete: notes.txt written and verified.", Cost: 0.003},
	)
	defer srv.Close()

	ag, out, sdir, statsDir := newTestAgent(t, srv, ModeAgent)
	if err := ag.RunTurn(context.Background(), "create and verify a notes file"); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	// the tool ran inside a subagent
	if b, err := os.ReadFile(target); err != nil || string(b) != "orchestrated\n" {
		t.Fatalf("subagent did not write file: %q err=%v", string(b), err)
	}
	// plan and synthesis are visible
	o := out.String()
	for _, want := range []string{"plan (2 tasks)", "subagent 1/2", "subagent 2/2", "Both tasks complete"} {
		if !strings.Contains(o, want) {
			t.Errorf("output missing %q:\n%s", want, o)
		}
	}
	// main session history stays compact and valid: system, user, assistant
	loaded, _ := session.Load(sdir, ag.Sess.ID)
	if len(loaded.Messages) != 3 || loaded.Messages[2].Role != "assistant" || len(loaded.Messages[2].ToolCalls) != 0 {
		t.Errorf("main history not compact: %d messages", len(loaded.Messages))
	}
	// stats recorded planner + subagent calls + synthesis with roles
	recs, _ := stats.Load(statsDir)
	roles := map[string]int{}
	for _, r := range recs {
		if r.Kind == "call" {
			roles[r.Role]++
		}
	}
	if roles["planner"] != 1 || roles["synthesis"] != 1 || roles["subagent"] != 3 {
		t.Errorf("stats roles = %v, want planner:1 subagent:3 synthesis:1", roles)
	}
	// subagent contexts were isolated: their requests contain the briefing,
	// while the synthesis request has no tool schemas
	if srv.Tools[len(srv.Tools)-1] != 0 {
		t.Error("synthesis call should carry no tools")
	}
}

// TestE2E_OrchestratorFallsBackOnSingleTask degrades to the normal loop when
// the planner returns one task.
func TestE2E_OrchestratorFallsBackOnSingleTask(t *testing.T) {
	srv := mockrouter.New(
		mockrouter.Step{Text: `["just answer"]`}, // planner: single task
		mockrouter.Step{Text: "direct answer"},   // normal loop reply
	)
	defer srv.Close()

	ag, out, sdir, _ := newTestAgent(t, srv, ModeAgent)
	if err := ag.RunTurn(context.Background(), "trivial thing"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "direct answer") {
		t.Errorf("missing direct answer: %s", out.String())
	}
	loaded, _ := session.Load(sdir, ag.Sess.ID)
	if len(loaded.Messages) != 3 {
		t.Errorf("history = %d messages, want 3", len(loaded.Messages))
	}
}

// TestE2E_ResumeRepairsDanglingToolCalls simulates a crash after the model
// requested a tool but before its result was recorded.
func TestE2E_ResumeRepairsDanglingToolCalls(t *testing.T) {
	sdir := t.TempDir()
	sess := session.New(sdir, "mock/model")
	sess.Messages = []api.Message{
		{Role: "system", Content: "old system prompt"},
		{Role: "user", Content: "list the files"},
		{Role: "assistant", ToolCalls: []api.ToolCall{{
			ID: "call_dangling", Type: "function",
			Function: api.FunctionCall{Name: "bash", Arguments: `{"command":"ls","description":"list"}`},
		}}},
	}
	if err := sess.Save(); err != nil {
		t.Fatal(err)
	}

	srv := mockrouter.New(mockrouter.Step{Text: "ok, continuing."})
	defer srv.Close()
	client := api.NewClient("test-key")
	client.BaseURL = srv.URL

	loaded, err := session.Load(sdir, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	ag := New(Options{Client: client, Model: "mock/model", Yolo: true, Sess: loaded, Out: &out})

	last := loaded.Messages[len(loaded.Messages)-1]
	if last.Role != "tool" || last.ToolCallID != "call_dangling" {
		t.Fatalf("dangling tool call was not repaired; tail = %+v", last)
	}
	if err := ag.RunTurn(context.Background(), "continue"); err != nil {
		t.Fatalf("RunTurn after repair: %v", err)
	}
	for i, m := range srv.Requests[0] {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			if i+1 >= len(srv.Requests[0]) || srv.Requests[0][i+1].Role != "tool" {
				t.Errorf("request still contains unanswered tool_calls at index %d", i)
			}
		}
	}
}

func jsonEsc(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return r.Replace(s)
}
