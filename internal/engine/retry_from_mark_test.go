package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writingWorker is a chapter worker that leaves a file in the repository and
// then reports what the test tells it to: the shape of an attempt stopped
// mid-work, or of one that finished.
type writingWorker struct {
	dir  string
	name string
	err  error
}

func (w *writingWorker) Work(_ context.Context, chapter Chapter, _ string) (WorkResult, error) {
	dir := w.dir
	if err := os.WriteFile(filepath.Join(dir, w.name), []byte("written by "+chapter.Title+"\n"), 0o600); err != nil {
		return WorkResult{}, err
	}
	if w.err != nil {
		return WorkResult{}, w.err
	}
	return WorkResult{Summary: chapter.Title}, nil
}

func realRunnerFor(repo string, worker *writingWorker) *SagaRunner {
	worker.dir = repo
	return &SagaRunner{
		Worker:   worker,
		Runner:   realRunner{},
		Detector: noGates{},
		Budget:   SagaBudget{MaxChapters: 10, CostLimit: 100},
		Write:    func(path string, data []byte, perm os.FileMode) error { return os.WriteFile(path, data, perm) },
		Now:      time.Now,
	}
}

// A chapter stopped mid-work -- Ctrl-C, a crash, a lost connection -- leaves
// whatever its worker had written in the tree. The next wake finds the chapter
// executing and retries it. The retry must start from the chapter's mark, not
// from the abandoned attempt, or the abandoned work rides into the retry's
// commit as if it had been meant.
func TestARetriedChapterStartsFromItsMarkNotFromAbandonedWork(t *testing.T) {
	repo := gitRepo(t)
	ctx := context.Background()
	state := &SagaState{Goal: "make it work", Status: "in_progress", Chapters: []Chapter{{Number: 1, Title: "first", Status: StatusPending}}}

	// Wake one: the worker writes and is stopped.
	stopped := &writingWorker{name: "abandoned.txt", err: context.Canceled}
	if _, err := realRunnerFor(repo, stopped).RunWake(ctx, repo, state); err == nil {
		t.Fatal("a stopped wake reported success")
	}
	if got := readFile(t, repo, "abandoned.txt"); !strings.HasPrefix(got, "written by") {
		t.Fatalf("setup: the abandoned file was not written: %q", got)
	}

	// Restart: a fresh process reads the artifact back.
	data, err := os.ReadFile(filepath.Join(repo, "SAGA.md"))
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := ParseSagaMarkdown(string(data))
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Chapters[0].Status != StatusExecuting || resumed.Chapters[0].Mark == nil {
		t.Fatalf("resumed chapter = %+v, want executing with its mark", resumed.Chapters[0])
	}

	// Wake two: the retry finishes the chapter and it is committed.
	if _, err := realRunnerFor(repo, &writingWorker{name: "real.txt"}).RunWake(ctx, repo, resumed); err != nil {
		t.Fatalf("retry wake: %v", err)
	}
	if got := readFile(t, repo, "abandoned.txt"); got != "<missing>" {
		t.Errorf("the abandoned attempt's file survived into the retry: %q", got)
	}
	committed := gitOut(t, repo, "show", "--name-only", "--format=", "HEAD")
	if strings.Contains(committed, "abandoned.txt") {
		t.Errorf("the retry's commit includes the abandoned work:\n%s", committed)
	}
	if !strings.Contains(committed, "real.txt") {
		t.Errorf("the retry's own work is not in its commit:\n%s", committed)
	}
	if _, err := os.Stat(filepath.Join(repo, "SAGA.md")); err != nil {
		t.Errorf("the saga's own artifact did not survive the retry: %v", err)
	}
}
