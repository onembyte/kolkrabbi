package engine

import (
	"context"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
)

// TestOnlyWritingSubagentsGetASnapshot pins the rule the plan settled on: a
// snapshot exists so a task that makes a mess can be taken back alone, and a
// task that changes no files can make no mess.
func TestOnlyWritingSubagentsGetASnapshot(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{Text: `[{"title":"read the code","kind":"research"},{"title":"change the code","kind":"edit"},{"title":"explain it","kind":"explain"}]`},
		enginetest.Step{Text: "read"},
		enginetest.Step{Text: "changed"},
		enginetest.Step{Text: "explained"},
		enginetest.Step{Text: "the final answer"},
	)
	defer srv.Close()

	agent, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	ckpt := &enginetest.FakeCheckpointer{}
	agent.Ckpt = ckpt
	agent.Effort = EffortHigh
	if err := agent.runOrchestrated(context.Background(), "do three things"); err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(ckpt.Tasks) != 1 || ckpt.Tasks[0] != "change the code" {
		t.Fatalf("snapshotted %v, want only the writing subagent", ckpt.Tasks)
	}
	// Bracketed, not merely opened: the paths a task changed are read when it
	// finishes, and a snapshot never closed records nothing to rewind.
	if len(ckpt.Ended) != 1 || ckpt.Ended[0] != 0 {
		t.Fatalf("ended %v, want the one handle that was opened", ckpt.Ended)
	}
}

// TestAFailedWritingSubagentIsStillSnapshotted is the case the feature exists
// for: a task that died half-way is exactly the one leaving a tree nobody
// asked for, and it is the one a snapshot must cover.
func TestAFailedWritingSubagentIsStillSnapshotted(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{Text: `[{"title":"break things","kind":"edit"},{"title":"read about it","kind":"research"}]`},
		enginetest.Step{StatusCode: 400, ErrorBody: `{"error":{"message":"model exploded"}}`},
		enginetest.Step{Text: "read"},
		enginetest.Step{Text: "the final answer"},
	)
	defer srv.Close()

	agent, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	ckpt := &enginetest.FakeCheckpointer{}
	agent.Ckpt = ckpt
	agent.Effort = EffortHigh
	if err := agent.runOrchestrated(context.Background(), "break things"); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(ckpt.Ended) != 1 {
		t.Fatalf("ended %v, want the failed task's snapshot closed too", ckpt.Ended)
	}
}
