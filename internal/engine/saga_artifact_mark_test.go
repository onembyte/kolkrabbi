package engine

import "testing"

// The mark rides in SAGA.md with the chapter, so a restart after a crash can
// still roll the chapter back to where it began.
func TestChapterMarkSurvivesTheArtifactRoundTrip(t *testing.T) {
	state := &SagaState{Goal: "g", Status: "in_progress", Chapters: []Chapter{{
		Number: 1, Title: "one", Status: StatusExecuting,
		Mark: &ChapterMark{Snapshot: "abc123", Untracked: []string{"notes.txt", "dir/scratch.md"}},
	}}}
	parsed, err := ParseSagaMarkdown(FormatSagaMarkdown(state))
	if err != nil {
		t.Fatal(err)
	}
	got := parsed.Chapters[0].Mark
	if got == nil || got.Snapshot != "abc123" || len(got.Untracked) != 2 || got.Untracked[1] != "dir/scratch.md" {
		t.Fatalf("mark after round trip = %+v", got)
	}
}
