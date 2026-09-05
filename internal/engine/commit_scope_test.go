package engine

import (
	"context"
	"strings"
	"testing"
)

// The user had an uncommitted edit when the chapter began. The chapter's
// commit is the chapter's: it holds what the chapter changed and the saga's
// artifact, and the user's edit stays exactly where it was -- uncommitted, in
// the tree, theirs to commit or not.
func TestAChapterCommitHoldsOnlyTheChaptersOwnChanges(t *testing.T) {
	repo := gitRepo(t)
	writeFile(t, repo, "user.txt", "the user's uncommitted edit\n")
	writeFile(t, repo, "user-notes.txt", "the user's untracked notes\n")
	state := &SagaState{Goal: "make it work", Status: "in_progress", Chapters: []Chapter{{Number: 1, Title: "first", Status: StatusPending}}}

	if _, err := realRunnerFor(repo, &writingWorker{name: "real.txt"}).RunWake(context.Background(), repo, state); err != nil {
		t.Fatalf("wake: %v", err)
	}
	committed := gitOut(t, repo, "show", "--name-only", "--format=", "HEAD")
	for _, theirs := range []string{"user.txt", "user-notes.txt"} {
		if strings.Contains(committed, theirs) {
			t.Errorf("the chapter commit swept in the user's %s:\n%s", theirs, committed)
		}
	}
	for _, ours := range []string{"real.txt", "SAGA.md"} {
		if !strings.Contains(committed, ours) {
			t.Errorf("the chapter commit is missing %s:\n%s", ours, committed)
		}
	}
	status := gitOut(t, repo, "status", "--porcelain")
	if !strings.Contains(status, " M user.txt") || !strings.Contains(status, "?? user-notes.txt") {
		t.Errorf("the user's own changes are not where they were:\n%s", status)
	}
}
