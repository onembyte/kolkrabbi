package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

func TestTheAgentWorkerChargesOnlyItsOwnTurn(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{Text: "chapter one done", Cost: 0.30},
		enginetest.Step{Text: "chapter two done", Cost: 0.45},
	)
	defer srv.Close()

	agent, _, _, _ := newTestAgentInternal(t, srv, ModeCode)
	worker := AgentWorker{Agent: agent}

	first, err := worker.Work(context.Background(), Chapter{Number: 1, Title: "one"}, "the goal")
	if err != nil {
		t.Fatal(err)
	}
	second, err := worker.Work(context.Background(), Chapter{Number: 2, Title: "two"}, "the goal")
	if err != nil {
		t.Fatal(err)
	}

	// A chapter charged the session's running total would make every chapter
	// look more expensive than the last, and the budget guard would stop a
	// saga early for spending it had already counted.
	if first.CostUSD < 0.29 || first.CostUSD > 0.31 {
		t.Fatalf("first chapter cost %.4f, want 0.30", first.CostUSD)
	}
	if second.CostUSD < 0.44 || second.CostUSD > 0.46 {
		t.Fatalf("second chapter cost %.4f, want 0.45", second.CostUSD)
	}
}

func TestTheAgentWorkerTellsTheModelTheGoalAndTheChapter(t *testing.T) {
	srv := enginetest.New(enginetest.Step{Text: "done"})
	defer srv.Close()

	agent, _, _, _ := newTestAgentInternal(t, srv, ModeCode)
	if _, err := (AgentWorker{Agent: agent}).Work(context.Background(),
		Chapter{Number: 2, Title: "wire the parser"}, "ship the compiler"); err != nil {
		t.Fatal(err)
	}

	var sent strings.Builder
	for _, message := range srv.Requests[0] {
		sent.WriteString(message.Content)
	}
	// A chapter without its goal is an instruction out of context; a goal
	// without the chapter is the whole project restated every turn.
	for _, want := range []string{"wire the parser", "ship the compiler"} {
		if !strings.Contains(sent.String(), want) {
			t.Fatalf("the turn did not mention %q", want)
		}
	}
}

func TestAFailedTurnIsAFailedChapter(t *testing.T) {
	srv := enginetest.New(enginetest.Step{StatusCode: 400, ErrorBody: `{"error":{"message":"nope"}}`})
	defer srv.Close()

	agent, _, _, _ := newTestAgentInternal(t, srv, ModeCode)
	if _, err := (AgentWorker{Agent: agent}).Work(context.Background(), Chapter{Number: 1, Title: "c"}, "g"); err == nil {
		t.Fatal("a failed turn reported success")
	}
}

func TestAnAgentWorkerNeedsAnAgent(t *testing.T) {
	if _, err := (AgentWorker{}).Work(context.Background(), Chapter{}, "g"); err == nil {
		t.Fatal("a worker with no agent reported success")
	}
}

var _ ChapterWorker = AgentWorker{}
var _ = provider.Message{}
