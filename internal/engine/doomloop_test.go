package engine

import "testing"

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
