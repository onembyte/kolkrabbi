package engine_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/bus"
	"github.com/onembyte/kolkrabbi/internal/checkpoint"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/session"
	"github.com/onembyte/kolkrabbi/internal/stats"
	"github.com/onembyte/kolkrabbi/internal/xid"
	"github.com/onembyte/kolkrabbi/protocol"
)

func newTestAgent(t *testing.T, srv *enginetest.Server, mode string) (*engine.Agent, *bytes.Buffer, string, string) {
	t.Helper()
	client := provider.NewClient("test-key")
	client.BaseURL = srv.URL
	sdir, statsDir := t.TempDir(), t.TempDir()
	sess := session.New(sdir, "mock/model")
	ckpt, err := checkpoint.Open(sess.CkptDir())
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	ag := engine.New(engine.Options{
		Client: client, Model: "mock/model", Mode: mode, Yolo: true,
		Sess: sess, Ckpt: ckpt, Out: &out, Recorder: stats.NewStore(statsDir),
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

	srv := enginetest.New(
		enginetest.Step{
			Text: "Creating the file now.",
			ToolCalls: []provider.ToolCall{{
				ID: "call_1",
				Function: provider.FunctionCall{
					Name:      "write_file",
					Arguments: `{"path":"` + jsonEsc(target) + `","content":"hi from kolk\n"}`,
				},
			}},
		},
		enginetest.Step{Text: "Done — hello.txt is created.", Cost: 0.002},
	)
	defer srv.Close()

	ag, out, sdir, statsDir := newTestAgent(t, srv, engine.ModeCode)
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
	if !strings.Contains(out.String(), "Writing file — "+target) || strings.Contains(out.String(), "write_file({") {
		t.Errorf("tool activity was not descriptive and payload-safe:\n%s", out.String())
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
	loaded, err := session.Load(sdir, ag.Sess.SessionID())
	if err != nil {
		t.Fatalf("session was not saved: %v", err)
	}
	if len(loaded.GetMessages()) != 5 {
		t.Fatalf("persisted %d messages, want 5", len(loaded.GetMessages()))
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
	if err != nil {
		t.Fatalf("Rewind: %v", err)
	}
	if len(restored) != 1 || restored[0] != target {
		t.Fatalf("rewound paths = %v, want [%s]", restored, target)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("target file still exists after rewind: %v", err)
	}
}

// TestE2E_ChatModeHasNoTools verifies chat mode never sends tool schemas,
// never executes tools, and answers purely conversationally.
func TestE2E_ChatModeHasNoTools(t *testing.T) {
	srv := enginetest.New(enginetest.Step{Text: "I am a helpful assistant."})
	defer srv.Close()

	ag, out, _, _ := newTestAgent(t, srv, engine.ModeChat)
	if err := ag.RunTurn(context.Background(), "hello"); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	if len(srv.Tools) != 1 || srv.Tools[0] != 0 {
		t.Fatalf("chat mode sent %v tool definitions to provider, want 0", srv.Tools)
	}
	if !strings.Contains(out.String(), "I am a helpful assistant.") {
		t.Errorf("output missing response: %s", out.String())
	}
}

// TestE2E_AgentModeOrchestratesTasks verifies agent mode: planner generates
// tasks -> subagents execute with isolated contexts -> synthesis produces
// the final answer -> main session history only sees user input + final answer.
func TestE2E_AgentModeOrchestratesTasks(t *testing.T) {
	work := t.TempDir()
	fileA := filepath.Join(work, "a.txt")
	fileB := filepath.Join(work, "b.txt")

	srv := enginetest.New(
		// 1. planner decomposes into two tasks
		enginetest.Step{Text: `["create a.txt with content A", "create b.txt with content B"]`},
		// 2. subagent 1: tool call -> result -> done
		enginetest.Step{
			Text: "Creating a.txt",
			ToolCalls: []provider.ToolCall{{
				ID: "call_a",
				Function: provider.FunctionCall{
					Name:      "write_file",
					Arguments: `{"path":"` + jsonEsc(fileA) + `","content":"A\n"}`,
				},
			}},
		},
		enginetest.Step{Text: "Created a.txt with content A."},
		// 3. subagent 2: tool call -> result -> done
		enginetest.Step{
			Text: "Creating b.txt",
			ToolCalls: []provider.ToolCall{{
				ID: "call_b",
				Function: provider.FunctionCall{
					Name:      "write_file",
					Arguments: `{"path":"` + jsonEsc(fileB) + `","content":"B\n"}`,
				},
			}},
		},
		enginetest.Step{Text: "Created b.txt with content B."},
		// 4. synthesis call
		enginetest.Step{Text: "Successfully created both a.txt and b.txt."},
	)
	defer srv.Close()

	ag, out, sdir, statsDir := newTestAgent(t, srv, engine.ModeAgent)
	if err := ag.RunTurn(context.Background(), "create two files a and b"); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	// files exist
	if b, err := os.ReadFile(fileA); err != nil || string(b) != "A\n" {
		t.Errorf("fileA content = %q, err=%v", string(b), err)
	}
	if b, err := os.ReadFile(fileB); err != nil || string(b) != "B\n" {
		t.Errorf("fileB content = %q, err=%v", string(b), err)
	}

	// out shows orchestration progress
	outStr := out.String()
	for _, want := range []string{"planning", "subagent 1/2", "subagent 2/2", "synthesizing", "Successfully created both"} {
		if !strings.Contains(outStr, want) {
			t.Errorf("agent mode output missing %q:\n%s", want, outStr)
		}
	}

	// main session only has system, user, assistant (synthesis): clean history
	loaded, err := session.Load(sdir, ag.Sess.SessionID())
	if err != nil {
		t.Fatal(err)
	}
	loadedMsgs := loaded.GetMessages()
	if len(loadedMsgs) != 3 {
		t.Fatalf("agent mode persisted %d messages, want 3 (system, user, synthesis)", len(loadedMsgs))
	}
	if loadedMsgs[2].Content != "Successfully created both a.txt and b.txt." {
		t.Errorf("persisted synthesis message = %q", loadedMsgs[2].Content)
	}

	// stats recorded all roles: planner, 2 subagents, synthesis
	recs, _ := stats.Load(statsDir)
	roles := map[string]int{}
	for _, r := range recs {
		if r.Kind == "call" {
			roles[r.Role]++
		}
	}
	if roles["planner"] != 1 || roles["subagent"] != 4 || roles["synthesis"] != 1 {
		t.Errorf("stats roles = %+v, want 1 planner, 4 subagent, 1 synthesis", roles)
	}
}

// TestE2E_DanglingToolCallsRepairedOnStartup simulates an interrupted run
// where the previous process died after sending tool_calls but before results
// were written to the session. The next startup must repair it so the API
// does not reject the history.
func TestE2E_DanglingToolCallsRepairedOnStartup(t *testing.T) {
	sdir := t.TempDir()
	sess := session.New(sdir, "mock/model")
	sess.SetMessages([]provider.Message{
		{Role: "system", Content: "old system prompt"},
		{Role: "user", Content: "list the files"},
		{Role: "assistant", ToolCalls: []provider.ToolCall{{
			ID: "call_dangling", Type: "function",
			Function: provider.FunctionCall{Name: "bash", Arguments: `{"command":"ls","description":"list"}`},
		}}},
	})
	if err := sess.Save(); err != nil {
		t.Fatal(err)
	}

	srv := enginetest.New(enginetest.Step{Text: "ok, continuing."})
	defer srv.Close()
	client := provider.NewClient("test-key")
	client.BaseURL = srv.URL

	loaded, err := session.Load(sdir, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	ag := engine.New(engine.Options{Client: client, Model: "mock/model", Yolo: true, Sess: loaded, Out: &out})

	loadedMsgs := loaded.GetMessages()
	last := loadedMsgs[len(loadedMsgs)-1]
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

func TestE2E_RunTurnEmitsProtocolEventsToBus(t *testing.T) {
	srv := enginetest.New(enginetest.Step{Text: "Hello from bus!"})
	defer srv.Close()

	client := provider.NewClient("test-key")
	client.BaseURL = srv.URL

	sdir := t.TempDir()
	sessID := xid.New(xid.Session)
	sess := session.New(sdir, "mock/model")
	sess.ID = sessID

	b, err := bus.New(sessID, bus.Options{})
	if err != nil {
		t.Fatalf("bus.New: %v", err)
	}

	sub, err := b.Subscribe(0)
	if err != nil {
		t.Fatalf("b.Subscribe: %v", err)
	}

	var out bytes.Buffer
	ag := engine.New(engine.Options{
		Client: client,
		Model:  "mock/model",
		Yolo:   true,
		Sess:   sess,
		Out:    &out,
		Bus:    b,
	})

	if err := ag.RunTurn(context.Background(), "hi"); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	var eventTypes []protocol.EventType
	for len(sub.Events()) > 0 {
		env := <-sub.Events()
		eventTypes = append(eventTypes, env.Type)
	}

	if len(eventTypes) == 0 {
		t.Fatal("no events received on the bus")
	}

	hasTurnStarted := false
	hasMessageDelta := false
	hasTurnFinished := false
	for _, et := range eventTypes {
		if et == protocol.EventTurnStarted {
			hasTurnStarted = true
		}
		if et == protocol.EventMessageDelta {
			hasMessageDelta = true
		}
		if et == protocol.EventTurnFinished {
			hasTurnFinished = true
		}
	}

	if !hasTurnStarted || !hasMessageDelta || !hasTurnFinished {
		t.Fatalf("missing required turn events, received: %v", eventTypes)
	}
}
