package engine_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/engine"
)

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

func TestVerifyAndCommitRunsGatesThenCommits(t *testing.T) {
	runner := &recordingRunner{
		results: map[string]engine.CommandResult{
			"go test ./...": {},
			"make check":    {},
			"git add -A && git commit -m 'saga(chapter 2): update checks'": {},
			"git rev-parse --short HEAD":                                   {Output: "abc123\n"},
		},
		errs: map[string]error{},
	}
	err := engine.VerifyAndCommit(context.Background(), runner, t.TempDir(), []string{"go test ./...", "make check"}, engine.Chapter{Number: 2, Title: "update checks"})
	if err != nil {
		t.Fatalf("VerifyAndCommit() error = %v", err)
	}
	want := []string{"go test ./...", "make check", "git add -A && git commit -m 'saga(chapter 2): update checks'", "git rev-parse --short HEAD"}
	if len(runner.commands) != len(want) {
		t.Fatalf("commands = %#v, want %#v", runner.commands, want)
	}
	for i := range want {
		if runner.commands[i] != want[i] {
			t.Fatalf("command %d = %q, want %q", i, runner.commands[i], want[i])
		}
	}
}

func TestVerifyAndCommitRollsBackFailedGate(t *testing.T) {
	runner := &recordingRunner{results: map[string]engine.CommandResult{"go test ./...": {ExitCode: 1, Failure: "tests failed"}, "git checkout -- .": {}}, errs: map[string]error{}}
	err := engine.VerifyAndCommit(context.Background(), runner, t.TempDir(), []string{"go test ./..."}, engine.Chapter{Number: 1, Title: "failing chapter"})
	if err == nil || len(runner.commands) != 2 || runner.commands[1] != "git checkout -- ." {
		t.Fatalf("error/commands = %v/%#v", err, runner.commands)
	}
}

func TestVerifyAndCommitReportsRunnerFailureAndRollsBack(t *testing.T) {
	runner := &recordingRunner{results: map[string]engine.CommandResult{}, errs: map[string]error{"go test ./...": errors.New("runner unavailable")}}
	err := engine.VerifyAndCommit(context.Background(), runner, t.TempDir(), []string{"go test ./..."}, engine.Chapter{Number: 1, Title: "unavailable chapter"})
	if err == nil || len(runner.commands) != 2 || runner.commands[1] != "git checkout -- ." {
		t.Fatalf("error/commands = %v/%#v", err, runner.commands)
	}
}

func TestVerifyAndCommitReportsRollbackFailure(t *testing.T) {
	runner := &recordingRunner{results: map[string]engine.CommandResult{"go test ./...": {ExitCode: 1, Failure: "tests failed"}, "git checkout -- .": {ExitCode: 1, Failure: "checkout refused"}}, errs: map[string]error{}}
	err := engine.VerifyAndCommit(context.Background(), runner, t.TempDir(), []string{"go test ./..."}, engine.Chapter{Number: 1, Title: "rollback"})
	if err == nil || !strings.Contains(err.Error(), "rollback failed") {
		t.Fatalf("error = %v, want rollback failure", err)
	}
}
