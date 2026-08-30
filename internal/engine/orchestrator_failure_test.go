package engine

import (
	"context"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
)

const planOfThree = `[{"title":"first"},{"title":"second"},{"title":"third"}]`

func TestOneFailedSubagentDoesNotThrowTheRunAway(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{Text: planOfThree},
		enginetest.Step{Text: "first is done"},
		enginetest.Step{StatusCode: http.StatusBadRequest, ErrorBody: `{"error":{"message":"model exploded"}}`},
		enginetest.Step{Text: "third is done"},
		enginetest.Step{Text: "the final answer"},
	)
	defer srv.Close()

	agent, out, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	agent.Effort = EffortHigh // the default width caps a plan at two tasks
	if err := agent.runOrchestrated(context.Background(), "do three things"); err != nil {
		t.Fatalf("the run aborted on one failure: %v", err)
	}

	// The two tasks that worked cost real money. Discarding them because a
	// third failed is the worst possible use of what was already spent.
	if !strings.Contains(out.String(), "the final answer") {
		t.Fatalf("no answer was produced; output:\n%s", out.String())
	}
	// Synthesis has to know, or the answer silently omits a third of the work.
	synthesis := lastRequestText(t, srv)
	if !strings.Contains(synthesis, "second") || !strings.Contains(strings.ToLower(synthesis), "fail") {
		t.Fatalf("synthesis was not told about the failure:\n%s", synthesis)
	}
	if !strings.Contains(synthesis, "first is done") || !strings.Contains(synthesis, "third is done") {
		t.Fatalf("synthesis lost the work that succeeded:\n%s", synthesis)
	}
}

func TestATaskWhoseDependencyFailedIsBlockedNotRun(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{Text: `[{"title":"find it"},{"title":"fix it","needs":[1]}]`},
		enginetest.Step{StatusCode: http.StatusBadRequest, ErrorBody: `{"error":{"message":"nope"}}`},
		enginetest.Step{Text: "the final answer"},
	)
	defer srv.Close()

	agent, out, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	if err := agent.runOrchestrated(context.Background(), "find and fix"); err != nil {
		t.Fatalf("run returned %v", err)
	}

	// Running "fix it" after "find it" failed spends money on a task that
	// cannot have the input it said it needed.
	if got := len(srv.Requests); got != 3 {
		t.Fatalf("%d requests, want plan + one failed subagent + synthesis", got)
	}
	if !strings.Contains(strings.ToLower(out.String()), "blocked") {
		t.Fatalf("the user was not told a task was skipped:\n%s", out.String())
	}
}

func TestATaskThatMerelyRanOutOfRoundsKeepsItsWork(t *testing.T) {
	agent, _, _, _ := newTestAgentInternal(t, enginetest.New(), ModeAgent)
	agent.Effort = EffortLow

	outcome := agent.classify("partial findings so far", errRoundsExhausted)

	if outcome.Status != statusIncomplete {
		t.Fatalf("status = %v, want incomplete", outcome.Status)
	}
	// Work that exists must not be thrown away for not being finished.
	if outcome.Result != "partial findings so far" {
		t.Fatalf("result = %q, want the partial work kept", outcome.Result)
	}
}

func TestEveryTaskFailingStillProducesAnAnswer(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{Text: `[{"title":"a"},{"title":"b"}]`},
		enginetest.Step{StatusCode: http.StatusBadRequest, ErrorBody: `{"error":{"message":"no"}}`},
		enginetest.Step{StatusCode: http.StatusBadRequest, ErrorBody: `{"error":{"message":"no"}}`},
		enginetest.Step{Text: "nothing worked, here is why"},
	)
	defer srv.Close()

	agent, out, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	if err := agent.runOrchestrated(context.Background(), "try"); err != nil {
		t.Fatalf("run returned %v", err)
	}
	if !strings.Contains(out.String(), "nothing worked") {
		t.Fatalf("no answer:\n%s", out.String())
	}
	// When most of a run failed, the interesting fact is not the synthesis.
	if !strings.Contains(strings.ToLower(out.String()), "2 of 2") {
		t.Fatalf("the user was not told how much failed:\n%s", out.String())
	}
}

func TestCancellationIsNotAFailureToReport(t *testing.T) {
	srv := enginetest.New(enginetest.Step{Text: "done"})
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	agent, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	cancel()

	// Straight at the delegation loop: going through the planner would fail
	// there and never reach the branch this test is about.
	outcomes, err := agent.runTasks(ctx, "a and b", []Task{{Title: "a"}, {Title: "b"}})

	// The user asked it to stop. Recording two failed tasks and synthesising an
	// answer would be answering a question they withdrew.
	if err == nil {
		t.Fatal("a cancelled run reported success")
	}
	if outcomes != nil {
		t.Fatalf("outcomes = %+v, want none — nothing here is worth reporting", outcomes)
	}
}

func TestAFailedTaskIsNamedInTheTerminalAsWellAsTheAnswer(t *testing.T) {
	srv := enginetest.New(
		enginetest.Step{Text: planOfThree},
		enginetest.Step{Text: "ok"},
		enginetest.Step{StatusCode: http.StatusBadRequest, ErrorBody: `{"error":{"message":"model exploded"}}`},
		enginetest.Step{Text: "ok"},
		enginetest.Step{Text: "answer"},
	)
	defer srv.Close()

	agent, out, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	agent.Effort = EffortHigh
	agent.runOrchestrated(context.Background(), "three things")

	got := out.String()
	if !strings.Contains(got, "second") || !strings.Contains(got, "model exploded") {
		t.Fatalf("the failure was not reported as it happened:\n%s", got)
	}
}

// lastRequestText flattens the final request's messages so a test can assert
// on what synthesis was actually told.
func lastRequestText(t *testing.T, srv *enginetest.Server) string {
	t.Helper()
	if len(srv.Requests) == 0 {
		t.Fatal("no requests were made")
	}
	var b strings.Builder
	for _, m := range srv.Requests[len(srv.Requests)-1] {
		b.WriteString(m.Content)
		b.WriteString("\n")
	}
	return b.String()
}

// A cancelled run must not strand the subagents that were still working.
//
// `finished` was unbuffered while runTasks returns from inside its loop on
// cancellation, so every goroutine that had not yet delivered its result
// blocked forever on the send. Today that leaks a goroutine; once a subagent
// owns a vendor process it leaks a child process per in-flight task, because
// the deferred Close never runs.
func TestACancelledRunLeavesNoSubagentGoroutineBehind(t *testing.T) {
	// Responses held open, so every task is still in flight when the
	// cancellation lands — the only state in which the leak exists.
	held := enginetest.Step{Text: "slow", Delay: 400 * time.Millisecond}
	srv := enginetest.New(held, held, held, held)
	defer srv.Close()

	agent, _, _, _ := newTestAgentInternal(t, srv, ModeAgent)
	agent.MaxConcurrentTasks = 4

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Research kinds, because a task that writes files is deliberately
		// serialised — with the zero Kind all four would run one at a time and
		// there would be nothing in flight to strand.
		_, _ = agent.runTasks(ctx, "four things", []Task{
			{Title: "a", Kind: KindResearch}, {Title: "b", Kind: KindResearch},
			{Title: "c", Kind: KindResearch}, {Title: "d", Kind: KindResearch},
		})
	}()

	// Let every task reach the server, then withdraw the question.
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	// Counted by name rather than by NumGoroutine: the runtime's total drifts
	// with whatever else the test binary is doing, and a leak of three inside
	// that noise is exactly what a blunt count misses.
	deadline := time.Now().Add(5 * time.Second)
	stranded := 0
	for time.Now().Before(deadline) {
		if stranded = countGoroutines("runOneTask"); stranded == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if stranded > 0 {
		t.Errorf("%d subagent goroutines outlived the cancelled run, blocked sending a result nobody will receive", stranded)
	}
}

// countGoroutines counts live goroutines whose stack names fn.
func countGoroutines(fn string) int {
	buffer := make([]byte, 1<<20)
	buffer = buffer[:runtime.Stack(buffer, true)]
	count := 0
	for _, stack := range strings.Split(string(buffer), "\n\n") {
		if strings.Contains(stack, fn) {
			count++
		}
	}
	return count
}
