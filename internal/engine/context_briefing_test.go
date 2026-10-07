package engine

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

func TestPlannerSeesTheConversationBehindARelativeFollowup(t *testing.T) {
	srv := enginetest.New(enginetest.Step{Text: `[{"title":"implement the agreed approach","kind":"edit"}]`})
	defer srv.Close()
	a, _, sess, _ := newTestAgentInternal(t, srv, ModeAgent)
	sess.AppendMessage(provider.Message{Role: "user", Content: "Preserve the current public API."})
	sess.AppendMessage(provider.Message{Role: "assistant", Content: "Proposal: add a transparent icon cache in graphics.go."})
	sess.AppendMessage(provider.Message{Role: "user", Content: "implement that"})
	if _, _, _, err := a.plan(context.Background(), a.SessionModel(), "implement that", 0); err != nil {
		t.Fatal(err)
	}
	request := srv.Requests[0][1].Content
	for _, want := range []string{"Preserve the current public API", "transparent icon cache", "implement that"} {
		if !strings.Contains(request, want) {
			t.Fatalf("planner lost %q", want)
		}
	}
}

func TestSynthesisOverflowShrinksEvidenceWithoutReplayingChildren(t *testing.T) {
	result := "edited graphics.go\n" + strings.Repeat("detailed evidence\n", 1500) + "tests passed"
	srv := enginetest.New(
		enginetest.Step{Text: `[{"title":"first","kind":"research"},{"title":"second","kind":"research"}]`},
		enginetest.Step{Text: result}, enginetest.Step{Text: result},
		enginetest.Step{StatusCode: http.StatusBadRequest, ErrorBody: `{"error":{"message":"context_length_exceeded"}}`},
		enginetest.Step{Text: "both completed"},
	)
	defer srv.Close()
	a, _, sess, _ := newTestAgentInternal(t, srv, ModeAgent)
	a.MaxConcurrentTasks = 1
	if err := a.RunTurn(context.Background(), "inspect both parts"); err != nil {
		t.Fatal(err)
	}
	if len(srv.Requests) != 5 || estimateTokens(srv.Requests[4]) >= estimateTokens(srv.Requests[3]) {
		t.Fatal("synthesis retried unchanged evidence or replayed child work")
	}
	for _, task := range sess.RunState().Tasks {
		if task.Result != result || task.Status != "done" {
			t.Fatal("synthesis preview replaced the complete stored result")
		}
	}
	if !strings.Contains(srv.Requests[4][1].Content, "2 done") {
		t.Fatal("shortened synthesis lost the complete outcome counts")
	}
}

func TestHugeResultSetsRetainOutcomeCountsAndDeclarePreviewLimits(t *testing.T) {
	var tasks []Task
	var outcomes []outcome
	for i := range 2000 {
		tasks = append(tasks, Task{Title: fmt.Sprintf("task %d", i)})
		outcomes = append(outcomes, outcome{Status: statusDone, Result: strings.Repeat("detail ", 100)})
	}
	outcomes[999] = outcome{Status: statusFailed, Reason: "verification failed"}
	got := boundedOutcomes(tasks, outcomes, 4096)
	if len(got) > 4400 || !strings.Contains(got, "1999 done") || !strings.Contains(got, "1 failed") || !strings.Contains(got, "evidence rows omitted") {
		t.Fatalf("large result preview is unbounded or conceals missing evidence: %d bytes", len(got))
	}
}

func TestChildWindowDoesNotInheritTheCeilingsLargerWindow(t *testing.T) {
	a := &Agent{Options: Options{ContextWindow: 200_000, Catalog: []provider.ModelInfo{{ID: "small", ContextLength: 4096}}}}
	if got := a.childWindow(pinnedBackend{}, "small"); got != 4096 {
		t.Fatalf("child window = %d", got)
	}
	if got := a.childWindow(pinnedBackend{}, "unknown"); got != 0 {
		t.Fatalf("unknown child inherited parent window: %d", got)
	}
}

func TestChildWindowUsesTheRoutedLocalRuntime(t *testing.T) {
	local := &windowBackend{routeBackend: routeBackend{name: "ollama"}, window: 4096}
	a := routedAgent(&routeBackend{name: "gateway"}, map[string]ChatBackend{"ollama": local})
	if got := a.childWindow(pinnedBackend{}, "ollama/small"); got != 4096 {
		t.Fatalf("routed child window = %d, want its runtime's effective size", got)
	}
}
