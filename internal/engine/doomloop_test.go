package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/enginetest"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

// Three consecutive calls with the same arguments AND the same result are not a
// fourth attempt, they are a loop (docs/plan/30-doom-loop-guard.md).
func TestALoopIsThreeIdenticalCallsWithIdenticalResults(t *testing.T) {
	var d doomLoop
	if d.observe("read_file", `{"path":"a.go"}`, "package a") {
		t.Error("the first call was called a loop")
	}
	if d.observe("read_file", `{"path":"a.go"}`, "package a") {
		t.Error("the second call was called a loop")
	}
	if !d.observe("read_file", `{"path":"a.go"}`, "package a") {
		t.Error("three identical calls with identical results were not caught")
	}
}

// The results half is the load-bearing one: a test that fails differently each
// run is progress, because the error is moving.
func TestAChangingResultIsProgressEvenWhenTheCommandIsTheSame(t *testing.T) {
	var d doomLoop
	for i, result := range []string{"FAIL: 3 tests", "FAIL: 2 tests", "FAIL: 1 test"} {
		if d.observe("bash", `{"command":"go test ./..."}`, result) {
			t.Errorf("call %d was called a loop while the failure was still moving", i+1)
		}
	}
}

// And the other way round: success is not the discriminator. Reading one file
// three times is waste even though every call returned fine.
func TestASucceedingCallCanStillBeALoop(t *testing.T) {
	var d doomLoop
	d.observe("read_file", `{"path":"go.mod"}`, "module x")
	d.observe("read_file", `{"path":"go.mod"}`, "module x")
	if !d.observe("read_file", `{"path":"go.mod"}`, "module x") {
		t.Error("a call that kept succeeding identically was not caught")
	}
}

// The false positive the naive rule would produce, excluded by the rule itself
// rather than by a special case.
func TestAnEditBetweenTwoTestRunsIsNotALoop(t *testing.T) {
	var d doomLoop
	d.observe("bash", `{"command":"go test ./..."}`, "FAIL")
	d.observe("edit_file", `{"path":"a.go","old":"x","new":"y"}`, "edited")
	d.observe("bash", `{"command":"go test ./..."}`, "FAIL")
	if d.observe("edit_file", `{"path":"a.go","old":"y","new":"z"}`, "edited") {
		t.Error("a model that tests, edits and tests again was called a loop")
	}
}

// Providers re-serialize the same call differently. A formatting difference is
// not a different intention.
func TestReserializationIsNotADifferentCall(t *testing.T) {
	var d doomLoop
	d.observe("read_file", `{"path":"a.go","limit":10}`, "x")
	d.observe("read_file", `{ "limit": 10, "path": "a.go" }`, "x")
	if !d.observe("read_file", "{\n  \"path\": \"a.go\",\n  \"limit\": 10\n}", "x") {
		t.Error("three spellings of one call were treated as three different calls")
	}
}

// Nothing else is normalized. An edit whose argument differs by one space is a
// different edit, and merging it with its neighbour would fire the guard on
// work that is progressing.
func TestNothingBeyondReserializationIsNormalized(t *testing.T) {
	var d doomLoop
	d.observe("edit_file", `{"old":"a b"}`, "ok")
	d.observe("edit_file", `{"old":"a  b"}`, "ok")
	if d.observe("edit_file", `{"old":"a b "}`, "ok") {
		t.Error("three different edits were merged into a loop by over-normalizing")
	}
}

// Arguments that are not valid JSON still have to be comparable: a model that
// sends the same malformed blob three times is looping too.
func TestMalformedArgumentsAreComparedAsText(t *testing.T) {
	var d doomLoop
	d.observe("bash", "not json at all", "err")
	d.observe("bash", "not json at all", "err")
	if !d.observe("bash", "not json at all", "err") {
		t.Error("identical unparseable arguments were not compared")
	}
}

// The counter belongs to a turn. Asking for the same thing twice in two turns
// is a person repeating themselves, which is allowed.
func TestTheCounterResetsWithTheTurn(t *testing.T) {
	var d doomLoop
	d.observe("read_file", `{"path":"a"}`, "x")
	d.observe("read_file", `{"path":"a"}`, "x")
	d.reset()
	if d.observe("read_file", `{"path":"a"}`, "x") {
		t.Error("the count survived the turn that produced it")
	}
}

// Once a loop is reported, the caller decides what happens; the detector must
// not report the same loop again on the next identical call, or a single stuck
// model would produce a prompt per round.
func TestALoopIsReportedOnceUntilSomethingChanges(t *testing.T) {
	var d doomLoop
	d.observe("read_file", `{"path":"a"}`, "x")
	d.observe("read_file", `{"path":"a"}`, "x")
	if !d.observe("read_file", `{"path":"a"}`, "x") {
		t.Fatal("the third call was not caught")
	}
	if d.observe("read_file", `{"path":"a"}`, "x") {
		t.Error("the same loop was reported twice")
	}
}

// The decision says the third identical call is never executed, so the check
// has to happen before the call runs — while still using the result half of the
// rule, which can only come from the two calls that already settled.
func TestAPendingCallIsRecognisedBeforeItRuns(t *testing.T) {
	var d doomLoop
	if d.wouldRepeat("read_file", `{"path":"a"}`) {
		t.Error("the very first call was blocked before running")
	}
	d.observe("read_file", `{"path":"a"}`, "x")
	if d.wouldRepeat("read_file", `{"path":"a"}`) {
		t.Error("the second call was blocked after only one identical result")
	}
	d.observe("read_file", `{"path":"a"}`, "x")
	if !d.wouldRepeat("read_file", `{"path":"a"}`) {
		t.Error("the third identical call was allowed to run")
	}
}

// Two identical calls whose results differed are not two-thirds of a loop.
func TestAPendingCallIsNotARepeatWhenTheResultsMoved(t *testing.T) {
	var d doomLoop
	d.observe("bash", `{"command":"go test"}`, "FAIL: 2")
	d.observe("bash", `{"command":"go test"}`, "FAIL: 1")
	if d.wouldRepeat("bash", `{"command":"go test"}`) {
		t.Error("a command whose output kept changing was called a loop")
	}
}

// "Run it again" has to mean it: the counter starts over, or the user is asked
// again on the very next call.
func TestAllowingARepeatClearsTheCount(t *testing.T) {
	var d doomLoop
	d.observe("read_file", `{"path":"a"}`, "x")
	d.observe("read_file", `{"path":"a"}`, "x")
	if !d.wouldRepeat("read_file", `{"path":"a"}`) {
		t.Fatal("setup did not reach the threshold")
	}
	d.allowRepeat()
	if d.wouldRepeat("read_file", `{"path":"a"}`) {
		t.Error("the user allowed the repeat and was asked about it again immediately")
	}
}

// The tiers differ in who is there to ask, so each needs its own behavioural
// test: the unit tests above prove the rule, not the wiring.

type scriptedDecider struct {
	answers []bool
	asked   []Confirmation
}

func (d *scriptedDecider) Confirm(_ context.Context, c Confirmation) bool {
	d.asked = append(d.asked, c)
	if len(d.answers) == 0 {
		return false
	}
	answer := d.answers[0]
	d.answers = d.answers[1:]
	return answer
}

func repeatedCallSteps(n int) []enginetest.Step {
	var steps []enginetest.Step
	for i := 0; i < n; i++ {
		steps = append(steps, enginetest.Step{
			Text: "again",
			ToolCalls: []provider.ToolCall{{
				ID: fmt.Sprintf("call_%d", i+1),
				Function: provider.FunctionCall{
					Name:      "read_file",
					Arguments: `{"path":"missing.txt"}`,
				},
			}},
		})
	}
	return steps
}

// Someone is watching: they are asked, and the only escape offered is this one
// call. There is deliberately no rule to keep — "always allow" here would mean
// "always allow me to spend your budget achieving nothing".
func TestAnInteractiveTierAsksAndOffersNoStandingRule(t *testing.T) {
	srv := enginetest.New(repeatedCallSteps(8)...)
	defer srv.Close()

	ag, _, _, _ := newTestAgentInternal(t, srv, ModeCode)
	ag.Permission = PermissionAsk
	decider := &scriptedDecider{answers: []bool{false}}
	ag.Decider = decider

	err := ag.RunTurn(context.Background(), "read the same missing file forever")
	var loop *DoomLoopError
	if !errors.As(err, &loop) {
		t.Fatalf("error = %v, want a DoomLoopError after the user declined", err)
	}
	if len(decider.asked) != 1 {
		t.Fatalf("the user was asked %d times, want once", len(decider.asked))
	}
	if decider.asked[0].Rule != "" {
		t.Errorf("a standing rule was offered for a doom loop: %q", decider.asked[0].Rule)
	}
	if !strings.Contains(decider.asked[0].Detail, "same result") {
		t.Errorf("the question does not say why it is being asked: %q", decider.asked[0].Detail)
	}
}

// Full-auto has nobody to ask, and "proceed anyway" is the behaviour the guard
// exists to prevent. It stops, and it says what it stopped.
func TestFullAutoStopsAndSaysWhat(t *testing.T) {
	srv := enginetest.New(repeatedCallSteps(8)...)
	defer srv.Close()

	ag, out, _, _ := newTestAgentInternal(t, srv, ModeCode)
	ag.Permission = PermissionFullAuto

	err := ag.RunTurn(context.Background(), "read the same missing file forever")
	var loop *DoomLoopError
	if !errors.As(err, &loop) {
		t.Fatalf("error = %v, want a DoomLoopError", err)
	}
	printed := out.String()
	for _, want := range []string{"read_file", "missing.txt"} {
		if !strings.Contains(printed, want) {
			t.Errorf("full-auto stopped without naming %q:\n%s", want, printed)
		}
	}
}

// A permission rule answers "is this dangerous?". This answers "is this
// futile?". A rule that allows everything must not disable it.
func TestAWideOpenRuleDoesNotDisableTheGuard(t *testing.T) {
	srv := enginetest.New(repeatedCallSteps(8)...)
	defer srv.Close()

	ag, _, _, _ := newTestAgentInternal(t, srv, ModeCode)
	ag.Permission = PermissionAsk
	ag.Rules = Rules{{Decision: VerdictAllow, Family: "*", Pattern: "*", Source: "allow *(*)"}}
	ag.Decider = &scriptedDecider{answers: []bool{false}}

	err := ag.RunTurn(context.Background(), "read the same missing file forever")
	var loop *DoomLoopError
	if !errors.As(err, &loop) {
		t.Fatalf("error = %v — an allow-everything rule silenced a spending guard", err)
	}
}
