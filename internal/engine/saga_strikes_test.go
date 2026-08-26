package engine_test

import (
	"testing"

	"github.com/onembyte/kolkrabbi/internal/engine"
)

func TestRecordGateFailureBlocksAtDefaultLimit(t *testing.T) {
	state := &engine.SagaState{}
	for i := 1; i <= engine.DefaultMaxStrikes; i++ {
		if err := engine.RecordGateFailure(state); err != nil {
			t.Fatalf("failure %d: %v", i, err)
		}
		if state.Strikes != i {
			t.Fatalf("strikes = %d, want %d", state.Strikes, i)
		}
	}
	if state.Status != "blocked" {
		t.Fatalf("status = %q, want blocked", state.Status)
	}
}

func TestRecordChapterSuccessResetsConsecutiveFailures(t *testing.T) {
	state := &engine.SagaState{Strikes: 2, MaxStrikes: 3, Status: "in-progress"}
	if err := engine.RecordChapterSuccess(state); err != nil {
		t.Fatal(err)
	}
	if state.Strikes != 0 {
		t.Fatalf("strikes = %d, want 0", state.Strikes)
	}
	if state.Status != "in-progress" {
		t.Fatalf("status = %q, want in-progress", state.Status)
	}
}

func TestRecordGateFailureHonorsConfiguredLimit(t *testing.T) {
	state := &engine.SagaState{MaxStrikes: 2}
	if err := engine.RecordGateFailure(state); err != nil {
		t.Fatal(err)
	}
	if state.Status == "blocked" {
		t.Fatal("status blocked before configured limit")
	}
	if err := engine.RecordGateFailure(state); err != nil {
		t.Fatal(err)
	}
	if state.Status != "blocked" {
		t.Fatalf("status = %q, want blocked", state.Status)
	}
}
