package engine_test

import (
	"context"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/engine"
)

// recordingRunner answers commands from a table and remembers the order.
// It outlived saga_checkpoint_test.go, which was deleted when the ad-hoc
// verification path was replaced by the ports design.
type recordingRunner struct {
	commands []string
	results  map[string]engine.CommandResult
	errs     map[string]error
}

func (r *recordingRunner) Run(_ context.Context, command, _ string) (engine.CommandResult, error) {
	r.commands = append(r.commands, command)
	if err := r.errs[command]; err != nil {
		return engine.CommandResult{}, err
	}
	return r.results[command], nil
}

// fixedGates is the detector a lifecycle test injects so the gates under test
// are the ones it named, not whatever markers a temp directory happens to have.
type fixedGates []engine.QualityGate

func (g fixedGates) Detect(string) []engine.QualityGate { return []engine.QualityGate(g) }

// verifierOver assembles the ports a lifecycle test needs around one runner.
func verifierOver(runner engine.CommandRunner, gates fixedGates) *engine.ChapterVerifier {
	ctx := context.Background()
	return &engine.ChapterVerifier{
		Detector:     gates,
		Runner:       engine.NewCommandGateRunner(ctx, runner),
		Checkpointer: engine.NewCommandCheckpointer(ctx, runner),
	}
}

func TestVerifyChapterCompletesAndResetsStrikes(t *testing.T) {
	runner := &recordingRunner{
		results: map[string]engine.CommandResult{
			"git status --porcelain": {Output: " M main.go\n"},
			"go test ./...":          {},
			"git add -A && git commit -m 'saga(chapter 1): first chapter'": {},
			"git rev-parse --short HEAD":                                   {Output: "def456\n"},
		},
		errs: map[string]error{},
	}
	state := &engine.SagaState{
		Strikes:  2,
		Chapters: []engine.Chapter{{Number: 1, Title: "first chapter", Status: engine.StatusExecuting}},
	}
	if err := engine.VerifyChapter(context.Background(), verifierOver(runner, fixedGates{{Name: "test", Command: "go test ./..."}}), t.TempDir(), state, 0); err != nil {
		t.Fatalf("VerifyChapter() error = %v", err)
	}
	chapter := state.Chapters[0]
	if chapter.Status != engine.StatusDone || state.Strikes != 0 {
		t.Fatalf("chapter/state = %q/%d, want completed/0", chapter.Status, state.Strikes)
	}
	if chapter.Verification != "quality gates passed" {
		t.Fatalf("verification = %q", chapter.Verification)
	}
	if chapter.Commit != "def456" {
		t.Fatalf("commit = %q, want def456", chapter.Commit)
	}
}

func TestVerifyChapterMarksFailureAndBlocksAtLimit(t *testing.T) {
	runner := &recordingRunner{
		results: map[string]engine.CommandResult{
			"git status --porcelain": {Output: " M main.go\n"},
			"go test ./...":          {ExitCode: 1, Failure: "tests failed"},
			"git checkout -- .":      {},
		},
		errs: map[string]error{},
	}
	state := &engine.SagaState{
		Strikes:    2,
		MaxStrikes: 3,
		Chapters:   []engine.Chapter{{Number: 1, Title: "broken chapter", Status: engine.StatusVerifying}},
	}
	if err := engine.VerifyChapter(context.Background(), verifierOver(runner, fixedGates{{Name: "test", Command: "go test ./..."}}), t.TempDir(), state, 0); err == nil {
		t.Fatal("VerifyChapter() error = nil, want gate failure")
	}
	if state.Chapters[0].Status != engine.StatusFailed || state.Status != "blocked" || state.Strikes != 3 {
		t.Fatalf("state = %+v, want failed chapter and blocked saga at strike 3", state)
	}
}

func TestVerifyChapterRejectsInvalidChapter(t *testing.T) {
	state := &engine.SagaState{
		Chapters: []engine.Chapter{{Number: 1, Status: engine.StatusDone}},
	}

	err := engine.VerifyChapter(context.Background(), verifierOver(&recordingRunner{}, fixedGates{}), t.TempDir(), state, 0)
	if err == nil {
		t.Fatal("VerifyChapter() error = nil, want invalid-status error")
	}
}
