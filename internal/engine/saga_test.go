package engine_test

import (
	"testing"
	"time"

	"github.com/onembyte/kolkrabbi/internal/engine"
)

func TestSagaMarkdownRoundTrip(t *testing.T) {
	started, _ := time.Parse("2006-01-02 15:04:05", "2026-08-26 00:40:12")
	original := &engine.SagaState{
		Goal:           "Replace cgo sqlite driver with pure-Go modernc.org/sqlite and ensure zero cgo build",
		Started:        started,
		Status:         "in-progress",
		ActiveChapter:  3,
		MaxChapters:    15,
		CumulativeCost: 0.07,
		CostLimit:      5.00,
		Strikes:        1,
		MaxStrikes:     3,
		Criteria: []engine.AcceptanceCriterion{
			{Description: "internal/store compiles without CGO_ENABLED=1", Done: true},
			{Description: "All unit and integration store tests pass", Done: true},
			{Description: "./scripts/test.sh passes with 0 failures", Done: false},
			{Description: "make platforms compiles clean on all 5 targets", Done: false},
		},
		Chapters: []engine.Chapter{
			{
				Number:       1,
				Title:        "Dependency audit and schema inspection",
				Status:       engine.StatusDone,
				Changes:      []string{"Read internal/store/db.go and go.mod. No edits."},
				Verification: "Clean.",
				CostUSD:      0.02,
				DurationSec:  18,
			},
			{
				Number:       2,
				Title:        "Switch driver to modernc.org/sqlite",
				Status:       engine.StatusDone,
				Changes:      []string{"internal/store/db.go", "go.mod", "go.sum"},
				Verification: "go test -v ./internal/store passed (14 tests).",
				Commit:       "a3f912c",
				CostUSD:      0.05,
				DurationSec:  42,
			},
		},
		OpenRisks: []string{
			"Ensure Windows amd64 cross-compilation passes without gcc.",
		},
	}

	markdown := engine.FormatSagaMarkdown(original)
	parsed, err := engine.ParseSagaMarkdown(markdown)
	if err != nil {
		t.Fatalf("ParseSagaMarkdown failed: %v", err)
	}

	if parsed.Goal != original.Goal {
		t.Errorf("Goal = %q, want %q", parsed.Goal, original.Goal)
	}
	if parsed.Status != original.Status {
		t.Errorf("Status = %q, want %q", parsed.Status, original.Status)
	}
	if parsed.ActiveChapter != original.ActiveChapter {
		t.Errorf("ActiveChapter = %d, want %d", parsed.ActiveChapter, original.ActiveChapter)
	}
	if parsed.MaxChapters != original.MaxChapters {
		t.Errorf("MaxChapters = %d, want %d", parsed.MaxChapters, original.MaxChapters)
	}
	if parsed.Strikes != original.Strikes || parsed.MaxStrikes != original.MaxStrikes {
		t.Errorf("strikes = %d/%d, want %d/%d", parsed.Strikes, parsed.MaxStrikes, original.Strikes, original.MaxStrikes)
	}
	if len(parsed.Criteria) != len(original.Criteria) {
		t.Fatalf("Criteria count = %d, want %d", len(parsed.Criteria), len(original.Criteria))
	}
	for i, c := range parsed.Criteria {
		if c.Description != original.Criteria[i].Description || c.Done != original.Criteria[i].Done {
			t.Errorf("Criterion %d = %+v, want %+v", i, c, original.Criteria[i])
		}
	}
	if len(parsed.Chapters) != len(original.Chapters) {
		t.Fatalf("Chapters count = %d, want %d", len(parsed.Chapters), len(original.Chapters))
	}
	if parsed.Chapters[1].Commit != "a3f912c" {
		t.Errorf("Chapter 2 commit = %q, want a3f912c", parsed.Chapters[1].Commit)
	}
}

func TestValidateChapterTransition(t *testing.T) {
	// Legal happy path
	validSteps := []struct {
		from, to engine.ChapterStatus
	}{
		{engine.StatusPending, engine.StatusPlanning},
		{engine.StatusPlanning, engine.StatusExecuting},
		{engine.StatusExecuting, engine.StatusVerifying},
		{engine.StatusVerifying, engine.StatusDone},
		// Repair loop
		{engine.StatusVerifying, engine.StatusFailed},
		{engine.StatusFailed, engine.StatusPlanning},
	}

	for _, step := range validSteps {
		if err := engine.ValidateTransition(step.from, step.to); err != nil {
			t.Errorf("expected transition %q -> %q to be valid, got: %v", step.from, step.to, err)
		}
	}

	// Illegal steps
	invalidSteps := []struct {
		from, to engine.ChapterStatus
	}{
		{engine.StatusPending, engine.StatusDone},      // cannot skip execution and verification
		{engine.StatusExecuting, engine.StatusDone},    // cannot skip verification
		{engine.StatusDone, engine.StatusPlanning},     // terminal cannot transition
		{engine.StatusAborted, engine.StatusExecuting}, // terminal cannot transition
	}

	for _, step := range invalidSteps {
		if err := engine.ValidateTransition(step.from, step.to); err == nil {
			t.Errorf("expected transition %q -> %q to fail", step.from, step.to)
		}
	}
}
