package engine

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

func longTaskContext() []provider.Message {
	msgs := []provider.Message{
		{Role: "system", Content: "Keep the project constraints."},
		{Role: "user", Content: "Implement purple octopus loading; keep the existing API."},
	}
	for _, id := range []string{"first", "second", "latest"} {
		msgs = append(msgs,
			provider.Message{Role: "assistant", Content: "Decision: preserve the terminal fallback.", ToolCalls: []provider.ToolCall{{ID: id, Function: provider.FunctionCall{Name: "read", Arguments: `{"path":"internal/tui/icon.go"}`}}}},
			provider.Message{Role: "tool", ToolCallID: id, Content: id + strings.Repeat(" source line ", 2500) + " final diagnostic"},
		)
	}
	return msgs
}

func TestCompactCurrentToolRoundsPreservesGoalAndLatestTransaction(t *testing.T) {
	before := longTaskContext()
	sess := enginetest.NewFakeSession("s1", "vendor/model")
	sess.SetMessages(before)
	var archived []provider.Message
	a := New(Options{Sess: sess, Out: &strings.Builder{}, ArchiveCompaction: func(msgs []provider.Message) (string, error) {
		archived = msgs
		return "saved-history.json", nil
	}})
	sess.SetMessages(before)
	result, changed := a.CompactNow(context.Background(), estimateTokens(before)/2)
	if !changed || result.FreedTokens <= 0 {
		t.Fatal("one long user turn could not compact completed tool rounds")
	}
	if !reflect.DeepEqual(archived, before) {
		t.Fatal("full history was not archived before compaction")
	}
	got := sess.GetMessages()
	if !reflect.DeepEqual(got[:2], before[:2]) || !reflect.DeepEqual(got[len(got)-2:], before[len(before)-2:]) {
		t.Fatal("compaction changed the goal or latest tool transaction")
	}
	assertWellFormed(t, got)
	for _, message := range got {
		if message.Role == "assistant" && !strings.Contains(message.Content, "preserve the terminal fallback") {
			t.Fatal("compaction lost the agent's decision")
		}
	}
}

func TestContextArchiveFailureKeepsConversation(t *testing.T) {
	a, sess, _ := compactionAgent(t, 20_000, 19_000)
	before := sess.GetMessages()
	a.ArchiveCompaction = func([]provider.Message) (string, error) { return "", errors.New("disk full") }
	if _, changed := a.CompactNow(context.Background(), 10_000); changed {
		t.Fatal("reported compaction after archive failure")
	}
	if !reflect.DeepEqual(before, sess.GetMessages()) {
		t.Fatal("archive failure discarded the only complete history")
	}
}

func TestOverflowRecoveryShrinksDespiteStaleLargeWindow(t *testing.T) {
	a, sess, _ := compactionAgent(t, 2_000_000, 0)
	sess.SetMessages(longTaskContext())
	before := estimateTokens(sess.GetMessages())
	if !a.recoverFromOverflow(context.Background()) || estimateTokens(sess.GetMessages()) >= before {
		t.Fatal("stale advertised window prevented overflow recovery")
	}
}

func TestContextSaveFailureKeepsConversation(t *testing.T) {
	before := longTaskContext()
	sess := &failingSaveSession{messages: before}
	a := &Agent{Options: Options{Sess: sess, Out: &strings.Builder{}}}
	if _, changed := a.CompactNow(context.Background(), estimateTokens(before)/2); changed {
		t.Fatal("reported success after the compacted session failed to save")
	}
	if !reflect.DeepEqual(before, sess.GetMessages()) || a.RestoreCompaction() {
		t.Fatal("save failure replaced the working history or created an undo")
	}
}

func TestRecentToolRoundsCompactWithoutSummaryProvider(t *testing.T) {
	msgs := append([]provider.Message{{Role: "system", Content: "rules"}, {Role: "user", Content: "first goal"}, {Role: "assistant", Content: "first answer"}}, longTaskContext()[1:]...)
	called := false
	result, err := CompactMessages(msgs, 2, estimateTokens(msgs)/2, func([]provider.Message) (string, error) {
		called = true
		return "", errors.New("summary provider unavailable")
	})
	if err != nil || called || result.FreedTokens <= 0 {
		t.Fatalf("local compaction unnecessarily required a summary: called=%v freed=%d err=%v", called, result.FreedTokens, err)
	}
}

func TestCompactionPreservesAnUnfinishedBatch(t *testing.T) {
	msgs := longTaskContext()
	msgs = append(msgs, provider.Message{Role: "assistant", ToolCalls: []provider.ToolCall{{ID: "done"}, {ID: "pending"}}}, provider.Message{Role: "tool", ToolCallID: "done", Content: "side effect completed"})
	result, err := CompactMessages(msgs, 2, 10, nil)
	if err != nil || !reflect.DeepEqual(result.Messages, msgs) {
		t.Fatal("a pending batch was changed by compaction")
	}
}

func TestSummaryReceivesOriginalToolFacts(t *testing.T) {
	msgs := longSession()
	msgs[2].Content = "DECISION preserve the config format"
	msgs[3].Content = "edited important.go " + msgs[3].Content
	_, err := CompactMessages(msgs, 1, 10, func(span []provider.Message) (string, error) {
		if !strings.Contains(span[2].Content, "DECISION") || !strings.Contains(span[3].Content, "important.go") || len(span[2].ToolCalls) == 0 {
			t.Fatal("summary received evidence already discarded by a cheaper stage")
		}
		return "keep the config format; important.go edited", nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCompactionUndoRetainsProgressMadeAfterTheShrink(t *testing.T) {
	a, sess, _ := compactionAgent(t, 20_000, 19_000)
	a.compactIfNeeded(context.Background())
	sess.AppendMessage(provider.Message{Role: "user", Content: "follow-up goal"})
	sess.AppendMessage(provider.Message{Role: "assistant", Content: "follow-up completed"})
	if !a.RestoreCompaction() {
		t.Fatal("could not restore compaction")
	}
	msgs := sess.GetMessages()
	if msgs[len(msgs)-1].Content != "follow-up completed" || msgs[len(msgs)-2].Content != "follow-up goal" {
		t.Fatal("undo discarded work completed after compaction")
	}
}
