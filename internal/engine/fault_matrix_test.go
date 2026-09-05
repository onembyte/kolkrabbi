package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fault matrix, on a real repository. Stop and restart are proven in
// retry_from_mark_test.go; here are failed verification and a persistence
// failure, each followed by the wake that comes after, and each asserting the
// one thing the phase is for: no later commit holds abandoned work.

// Failed verification: the chapter's gate rejects it, the chapter is rolled
// back, and the retry's commit holds the retry's work and none of the failed
// attempt's.
func TestAFailedVerificationLeavesNothingOfTheAttemptInTheNextCommit(t *testing.T) {
	repo := gitRepo(t)
	ctx := context.Background()
	head := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	state := &SagaState{Goal: "make it work", Status: "in_progress", MaxStrikes: 3,
		Chapters: []Chapter{{Number: 1, Title: "first", Status: StatusPending}}}
	gate := fixedDetector{{Name: "marker", Command: "test -f pass.marker"}}

	failing := realRunnerFor(repo, &writingWorker{name: "failed-attempt.txt"})
	failing.Detector = gate
	if _, err := failing.RunWake(ctx, repo, state); err == nil {
		t.Fatal("a chapter whose gate failed reported success")
	}
	if got := readFile(t, repo, "failed-attempt.txt"); got != "<missing>" {
		t.Errorf("the failed attempt's file survived its rollback: %q", got)
	}
	if now := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD")); now != head {
		t.Errorf("a failed chapter moved HEAD from %s to %s", head, now)
	}

	passing := realRunnerFor(repo, &writingWorker{name: "pass.marker"})
	passing.Detector = gate
	if _, err := passing.RunWake(ctx, repo, state); err != nil {
		t.Fatalf("retry wake: %v", err)
	}
	committed := gitOut(t, repo, "show", "--name-only", "--format=", "HEAD")
	if strings.Contains(committed, "failed-attempt.txt") {
		t.Errorf("the retry's commit holds the failed attempt's work:\n%s", committed)
	}
	if !strings.Contains(committed, "pass.marker") {
		t.Errorf("the retry's own work is not in its commit:\n%s", committed)
	}
}

// Persistence failure after the commit: the chapter was committed, then the
// artifact could not be written, so the disk still says executing. The
// restart must not roll the committed work back out of the tree and redo it;
// it must find the chapter already done.
func TestAPersistenceFailureAfterCommitDoesNotUndoTheCommittedChapterOnRestart(t *testing.T) {
	repo := gitRepo(t)
	ctx := context.Background()
	state := &SagaState{Goal: "make it work", Status: "in_progress", MaxStrikes: 3,
		Chapters: []Chapter{{Number: 1, Title: "first", Status: StatusPending}}}

	writes := 0
	first := realRunnerFor(repo, &writingWorker{name: "real.txt"})
	first.Write = func(path string, data []byte, perm os.FileMode) error {
		writes++
		if writes == 2 { // the persist after verification and commit
			return os.ErrPermission
		}
		return os.WriteFile(path, data, perm)
	}
	if _, err := first.RunWake(ctx, repo, state); err == nil {
		t.Fatal("a wake whose artifact write failed reported success")
	}
	chapterCommit := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	if committed := gitOut(t, repo, "show", "--name-only", "--format=", "HEAD"); !strings.Contains(committed, "real.txt") {
		t.Fatalf("setup: the chapter was not committed before the persist failure:\n%s", committed)
	}

	data, err := os.ReadFile(filepath.Join(repo, "SAGA.md"))
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := ParseSagaMarkdown(string(data))
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Chapters[0].Status != StatusExecuting {
		t.Fatalf("setup: the artifact on disk says %q, want executing (the last write failed)", resumed.Chapters[0].Status)
	}

	second := realRunnerFor(repo, &writingWorker{name: "real.txt"}) // the retry finds the work in place
	if _, err := second.RunWake(ctx, repo, resumed); err != nil {
		t.Fatalf("restart wake: %v", err)
	}
	if got := readFile(t, repo, "real.txt"); !strings.HasPrefix(got, "written by") {
		t.Errorf("the committed chapter's file was rolled back on restart: %q", got)
	}
	// A commit that only brings SAGA.md up to date is the lost record being
	// written; a commit that touches anything else is the chapter redone.
	assertLaterCommitsTouchOnlyTheArtifact(t, repo, chapterCommit)
	if status := strings.TrimSpace(gitOut(t, repo, "status", "--porcelain")); strings.Contains(status, "real.txt") {
		t.Errorf("the committed chapter's file is dirty after restart:\n%s", status)
	}
}

// The same persistence failure over a dirty tree. Now the mark holds a
// snapshot -- the user's uncommitted edit was there at chapter start -- and a
// restart that rolled back to it would revert the committed chapter's files in
// the worktree and redo the chapter on top of its own commit.
func TestAPersistenceFailureAfterCommitOverADirtyTreeDoesNotRevertTheChapter(t *testing.T) {
	repo := gitRepo(t)
	ctx := context.Background()
	writeFile(t, repo, "user.txt", "the user's uncommitted edit\n")
	state := &SagaState{Goal: "make it work", Status: "in_progress", MaxStrikes: 3,
		Chapters: []Chapter{{Number: 1, Title: "first", Status: StatusPending}}}

	writes := 0
	first := realRunnerFor(repo, &writingWorker{name: "real.txt"})
	first.Write = func(path string, data []byte, perm os.FileMode) error {
		writes++
		if writes == 2 {
			return os.ErrPermission
		}
		return os.WriteFile(path, data, perm)
	}
	if _, err := first.RunWake(ctx, repo, state); err == nil {
		t.Fatal("a wake whose artifact write failed reported success")
	}
	chapterCommit := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))

	data, err := os.ReadFile(filepath.Join(repo, "SAGA.md"))
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := ParseSagaMarkdown(string(data))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := realRunnerFor(repo, &writingWorker{name: "real.txt"}).RunWake(ctx, repo, resumed); err != nil {
		t.Fatalf("restart wake: %v", err)
	}
	assertLaterCommitsTouchOnlyTheArtifact(t, repo, chapterCommit)
	status := gitOut(t, repo, "status", "--porcelain")
	if strings.Contains(status, "real.txt") {
		t.Errorf("the committed chapter's file is dirty after restart (rolled back to the pre-commit snapshot):\n%s", status)
	}
	if !strings.Contains(status, " M user.txt") {
		t.Errorf("the user's uncommitted edit did not survive the restart:\n%s", status)
	}
}

// assertLaterCommitsTouchOnlyTheArtifact allows the restart one kind of commit
// after the chapter's own: the artifact catching up with the record it lost.
// Anything else in a later commit is the chapter's work committed twice.
func assertLaterCommitsTouchOnlyTheArtifact(t *testing.T, repo, chapterCommit string) {
	t.Helper()
	later := strings.Fields(gitOut(t, repo, "log", "--format=%H", chapterCommit+"..HEAD"))
	for _, commit := range later {
		files := strings.Fields(gitOut(t, repo, "show", "--name-only", "--format=", commit))
		for _, f := range files {
			if f != "SAGA.md" {
				t.Errorf("restart committed %s again in %s; the chapter was redone on top of its own commit", f, commit[:7])
			}
		}
	}
}
