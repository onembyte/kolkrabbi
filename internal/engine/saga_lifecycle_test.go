package engine_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/engine"
)

func TestVerifyChapterAndPersistWritesCompletedState(t *testing.T) {
	runner := &recordingRunner{
		results: map[string]engine.CommandResult{
			"git add -A && git commit -m 'saga(chapter 1): first chapter'": {},
			"git rev-parse --short HEAD":                                   {Output: "abc123"},
		},
		errs: map[string]error{},
	}

	var artifact string
	state := &engine.SagaState{Chapters: []engine.Chapter{{Number: 1, Title: "first chapter", Status: engine.StatusVerifying}}}
	err := engine.VerifyChapterAndPersist(context.Background(), runner, t.TempDir(), state, 0, nil,
		func(_ string, data []byte, _ os.FileMode) error {
			artifact = string(data)
			return nil
		})
	if err != nil {
		t.Fatalf("VerifyChapterAndPersist() error = %v", err)
	}

	if !strings.Contains(artifact, "**Status**: completed") || !strings.Contains(artifact, "abc123") {
		t.Fatalf("artifact does not contain completed chapter: %q", artifact)
	}
}

func TestVerifyChapterCompletesAndResetsStrikes(t *testing.T) {
	runner := &recordingRunner{
		results: map[string]engine.CommandResult{
			"go test ./...": {},
			"git add -A && git commit -m 'saga(chapter 1): first chapter'": {},
			"git rev-parse --short HEAD":                                   {Output: "def456\n"},
		},
		errs: map[string]error{},
	}
	state := &engine.SagaState{
		Strikes:  2,
		Chapters: []engine.Chapter{{Number: 1, Title: "first chapter", Status: engine.StatusExecuting}},
	}
	if err := engine.VerifyChapter(context.Background(), runner, t.TempDir(), state, 0, []string{"go test ./..."}); err != nil {
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
			"go test ./...":     {ExitCode: 1, Failure: "tests failed"},
			"git checkout -- .": {},
		},
		errs: map[string]error{},
	}
	state := &engine.SagaState{
		Strikes:    2,
		MaxStrikes: 3,
		Chapters:   []engine.Chapter{{Number: 1, Title: "broken chapter", Status: engine.StatusVerifying}},
	}
	if err := engine.VerifyChapter(context.Background(), runner, t.TempDir(), state, 0, []string{"go test ./..."}); err == nil {
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

	err := engine.VerifyChapter(context.Background(), &recordingRunner{}, t.TempDir(), state, 0, nil)
	if err == nil {
		t.Fatal("VerifyChapter() error = nil, want invalid-status error")
	}
}

func TestVerifyChapterAndPersistWritesFailedState(t *testing.T) {
	runner := &recordingRunner{
		results: map[string]engine.CommandResult{
			"make check":        {ExitCode: 1, Failure: "check failed"},
			"git checkout -- .": {},
		},
		errs: map[string]error{},
	}
	var artifact string
	state := &engine.SagaState{Chapters: []engine.Chapter{{Number: 1, Title: "broken chapter", Status: engine.StatusVerifying}}}
	err := engine.VerifyChapterAndPersist(context.Background(), runner, t.TempDir(), state, 0, []string{"make check"},
		func(_ string, data []byte, _ os.FileMode) error {
			artifact = string(data)
			return nil
		})
	if err == nil {
		t.Fatal("VerifyChapterAndPersist() error = nil, want gate failure")
	}
	if !strings.Contains(artifact, "**Status**: failed") || !strings.Contains(artifact, "Strikes") {
		t.Fatalf("artifact does not contain failed state: %q", artifact)
	}
}
