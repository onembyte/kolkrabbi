package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// scriptedRunner answers commands from a table and records what it was asked.
type scriptedRunner struct {
	replies map[string]CommandResult
	fail    map[string]error
	asked   []string
}

func (r *scriptedRunner) Run(_ context.Context, command, _ string) (CommandResult, error) {
	r.asked = append(r.asked, command)
	if err := r.fail[command]; err != nil {
		return CommandResult{}, err
	}
	if reply, ok := r.replies[command]; ok {
		return reply, nil
	}
	return CommandResult{}, nil
}

func TestGateRunnerReportsEachGateSeparately(t *testing.T) {
	runner := &scriptedRunner{replies: map[string]CommandResult{
		"go test ./...": {ExitCode: 1, Output: "FAIL", Failure: "exit 1"},
	}}
	gates := []QualityGate{{Name: "vet", Command: "go vet ./..."}, {Name: "test", Command: "go test ./..."}}

	results := NewCommandGateRunner(context.Background(), runner).RunGates("/repo", gates)

	if len(results) != 2 {
		t.Fatalf("got %d results, want one per gate", len(results))
	}
	if !results[0].Passed {
		t.Fatalf("vet reported failed: %+v", results[0])
	}
	// A run that stops at the first failure hides the rest, and the whole
	// point of naming gates separately is knowing which ones are broken.
	if results[1].Passed {
		t.Fatalf("a failing gate reported passed: %+v", results[1])
	}
	if !strings.Contains(results[1].Output, "FAIL") {
		t.Fatalf("the gate output was lost: %+v", results[1])
	}
}

func TestAGateThatCannotRunIsAFailedGateNotAPass(t *testing.T) {
	runner := &scriptedRunner{fail: map[string]error{"go vet ./...": errors.New("no shell")}}

	results := NewCommandGateRunner(context.Background(), runner).RunGates("/repo", []QualityGate{{Name: "vet", Command: "go vet ./..."}})

	if results[0].Passed {
		t.Fatal("a gate that could not run was treated as passing")
	}
	if !strings.Contains(results[0].Output, "no shell") {
		t.Fatalf("the reason was lost: %+v", results[0])
	}
}

func TestGatesStopWhenTheContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner := &scriptedRunner{}

	results := NewCommandGateRunner(ctx, runner).RunGates("/repo", []QualityGate{
		{Name: "a", Command: "one"}, {Name: "b", Command: "two"},
	})

	// `kolk saga stop` has to mean something while a gate is running.
	if len(runner.asked) != 0 {
		t.Fatalf("ran %v after cancellation", runner.asked)
	}
	if results[0].Passed {
		t.Fatal("a cancelled gate reported passing")
	}
}

func TestCommitReturnsTheShortHash(t *testing.T) {
	runner := &scriptedRunner{replies: map[string]CommandResult{
		"git rev-parse --short HEAD": {Output: "abc1234\n"},
	}}

	commit, err := NewCommandCheckpointer(context.Background(), runner).CommitChapter("/repo", 3, "add the parser", nil)
	if err != nil {
		t.Fatal(err)
	}
	if commit != "abc1234" {
		t.Fatalf("commit = %q", commit)
	}
	joined := strings.Join(runner.asked, " | ")
	if !strings.Contains(joined, "saga(chapter 3): add the parser") {
		t.Fatalf("the commit message did not name the chapter: %s", joined)
	}
}

func TestACommitMessageCannotEscapeItsQuotes(t *testing.T) {
	runner := &scriptedRunner{replies: map[string]CommandResult{
		"git rev-parse --short HEAD": {Output: "abc1234"},
	}}

	// A chapter title is model-written text on a shell command line.
	_, err := NewCommandCheckpointer(context.Background(), runner).CommitChapter("/repo", 1, "it's done'; rm -rf /; echo '", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, asked := range runner.asked {
		if strings.Contains(asked, "; rm -rf /") && !strings.Contains(asked, `'\''`) {
			t.Fatalf("a title broke out of its quoting: %s", asked)
		}
	}
}

func TestAnEmptyCommitHashIsAnError(t *testing.T) {
	runner := &scriptedRunner{replies: map[string]CommandResult{
		"git rev-parse --short HEAD": {Output: "   "},
	}}

	// Reporting a chapter as committed at revision "" is worse than failing.
	if _, err := NewCommandCheckpointer(context.Background(), runner).CommitChapter("/repo", 1, "t", nil); err == nil {
		t.Fatal("an empty hash was accepted")
	}
}

func TestHasChangesReadsThePorcelain(t *testing.T) {
	dirty := &scriptedRunner{replies: map[string]CommandResult{
		"git status --porcelain": {Output: " M internal/engine/agent.go\n"},
	}}
	clean := &scriptedRunner{replies: map[string]CommandResult{
		"git status --porcelain": {Output: "\n"},
	}}

	if changed, err := NewCommandCheckpointer(context.Background(), dirty).HasChanges("/repo", nil); err != nil || !changed {
		t.Fatalf("dirty tree: %v %v", changed, err)
	}
	if changed, err := NewCommandCheckpointer(context.Background(), clean).HasChanges("/repo", nil); err != nil || changed {
		t.Fatalf("clean tree: %v %v", changed, err)
	}
}

// Without a mark the rollback is conservative: tracked files back to HEAD and
// nothing else touched, because deleting untracked files without knowing
// which were the user's would be the destruction it exists to prevent.
func TestRollbackWithoutAMarkOnlyRestoresTrackedFiles(t *testing.T) {
	runner := &scriptedRunner{}

	if err := NewCommandCheckpointer(context.Background(), runner).RollbackChapter("/repo", nil); err != nil {
		t.Fatal(err)
	}
	if len(runner.asked) != 1 || !strings.Contains(runner.asked[0], "git checkout HEAD -- .") {
		t.Fatalf("rollback ran %v", runner.asked)
	}
}
