package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// gateScript answers a fixed number of failing runs, then passes.
type gateScript struct {
	failFirst int
	runs      int
	lastGates []QualityGate
}

func (g *gateScript) RunGates(_ string, gates []QualityGate) []GateResult {
	g.runs++
	g.lastGates = gates
	passed := g.runs > g.failFirst
	out := make([]GateResult, len(gates))
	for i, gate := range gates {
		out[i] = GateResult{Gate: gate, Passed: passed, Output: "tests failed: TestThing"}
	}
	return out
}

// repairSpy records that a repair was attempted and what it was told.
type repairSpy struct {
	calls  int
	output string
	err    error
}

func (r *repairSpy) Repair(_ context.Context, _ Chapter, gateOutput string) error {
	r.calls++
	r.output = gateOutput
	return r.err
}

type recordingCheckpointer struct {
	committed  int
	rolledBack int
}

func (c *recordingCheckpointer) CommitChapter(string, int, string) (string, error) {
	c.committed++
	return "abc1234", nil
}
func (c *recordingCheckpointer) RollbackChapter(string, *ChapterMark) error {
	c.rolledBack++
	return nil
}
func (c *recordingCheckpointer) MarkChapter(string) (ChapterMark, error) { return ChapterMark{}, nil }
func (c *recordingCheckpointer) HasChanges(string) (bool, error)         { return true, nil }

func verifierWith(gates *gateScript, repairer ChapterRepairer, ckpt *recordingCheckpointer) *ChapterVerifier {
	return &ChapterVerifier{
		Detector:     fixedDetector{{Name: "test", Command: "go test ./..."}},
		Runner:       gates,
		Checkpointer: ckpt,
		Repairer:     repairer,
	}
}

type fixedDetector []QualityGate

func (d fixedDetector) Detect(string) []QualityGate { return []QualityGate(d) }

func TestAFailedGateGetsOneRepairTurn(t *testing.T) {
	gates := &gateScript{failFirst: 1}
	repairer := &repairSpy{}
	ckpt := &recordingCheckpointer{}

	result, err := verifierWith(gates, repairer, ckpt).Verify(context.Background(), "/repo", Chapter{Number: 1, Title: "c"})
	if err != nil {
		t.Fatal(err)
	}

	// The doc gives a chapter one chance to fix its own regression before its
	// work is thrown away. Rolling back a nearly-right chapter is expensive:
	// the next attempt starts from nothing.
	if repairer.calls != 1 {
		t.Fatalf("repair called %d times, want once", repairer.calls)
	}
	if !result.Passed || ckpt.committed != 1 {
		t.Fatalf("a repaired chapter was not committed: %+v, commits %d", result, ckpt.committed)
	}
	if ckpt.rolledBack != 0 {
		t.Fatalf("a repaired chapter was rolled back anyway")
	}
}

func TestTheRepairTurnIsToldWhatFailed(t *testing.T) {
	repairer := &repairSpy{}
	verifierWith(&gateScript{failFirst: 1}, repairer, &recordingCheckpointer{}).
		Verify(context.Background(), "/repo", Chapter{Number: 1, Title: "c"})

	// "Fix the regression" is not an instruction without the regression.
	if !strings.Contains(repairer.output, "tests failed: TestThing") {
		t.Fatalf("the repair turn was told %q", repairer.output)
	}
}

func TestOnlyOneRepairTurn(t *testing.T) {
	gates := &gateScript{failFirst: 99} // never passes
	repairer := &repairSpy{}
	ckpt := &recordingCheckpointer{}

	result, err := verifierWith(gates, repairer, ckpt).Verify(context.Background(), "/repo", Chapter{Number: 1, Title: "c"})
	if err != nil {
		t.Fatal(err)
	}

	// One repair, not a loop: a chapter that cannot fix itself twice is a
	// chapter that will not fix itself at all, and each turn costs money.
	if repairer.calls != 1 {
		t.Fatalf("repair called %d times, want exactly once", repairer.calls)
	}
	if result.Passed || ckpt.rolledBack != 1 || ckpt.committed != 0 {
		t.Fatalf("a chapter that stayed broken was not rolled back: %+v", result)
	}
}

func TestARepairThatItselfFailsStillRollsBack(t *testing.T) {
	repairer := &repairSpy{err: errors.New("the model gave up")}
	ckpt := &recordingCheckpointer{}

	result, err := verifierWith(&gateScript{failFirst: 99}, repairer, ckpt).
		Verify(context.Background(), "/repo", Chapter{Number: 1, Title: "c"})

	// A repair that errors must not leave the broken work in the tree.
	if err != nil {
		t.Fatalf("a failed repair became a verifier error: %v", err)
	}
	if result.Passed || ckpt.rolledBack != 1 {
		t.Fatalf("result %+v, rollbacks %d", result, ckpt.rolledBack)
	}
}

func TestWithNoRepairerTheOldBehaviourHolds(t *testing.T) {
	gates := &gateScript{failFirst: 99}
	ckpt := &recordingCheckpointer{}

	result, err := verifierWith(gates, nil, ckpt).Verify(context.Background(), "/repo", Chapter{Number: 1, Title: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if gates.runs != 1 {
		t.Fatalf("gates ran %d times without a repairer", gates.runs)
	}
	if result.Passed || ckpt.rolledBack != 1 {
		t.Fatalf("result %+v", result)
	}
}

func TestAPassingChapterIsNeverRepaired(t *testing.T) {
	repairer := &repairSpy{}
	ckpt := &recordingCheckpointer{}

	result, _ := verifierWith(&gateScript{failFirst: 0}, repairer, ckpt).
		Verify(context.Background(), "/repo", Chapter{Number: 1, Title: "c"})

	if repairer.calls != 0 {
		t.Fatalf("a green chapter was repaired %d times", repairer.calls)
	}
	if !result.Passed || ckpt.committed != 1 {
		t.Fatalf("result %+v", result)
	}
}
