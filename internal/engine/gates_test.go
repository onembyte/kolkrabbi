package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/engine"
)

// --- FileGateDetector tests ---

func TestDetectGoProject(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x"), 0644); err != nil {
		t.Fatal(err)
	}

	gates := engine.FileGateDetector{}.Detect(dir)
	if len(gates) != 1 {
		t.Fatalf("got %d gates, want 1", len(gates))
	}
	if gates[0].Name != "go" {
		t.Errorf("gate name = %q, want go", gates[0].Name)
	}
	if gates[0].Command != "go vet ./... && go test ./..." {
		t.Errorf("gate command = %q", gates[0].Command)
	}
}

func TestDetectNodeProject(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	gates := engine.FileGateDetector{}.Detect(dir)
	if len(gates) != 1 {
		t.Fatalf("got %d gates, want 1", len(gates))
	}
	if gates[0].Name != "node" {
		t.Errorf("gate name = %q, want node", gates[0].Name)
	}
}

func TestDetectRustProject(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]"), 0644); err != nil {
		t.Fatal(err)
	}

	gates := engine.FileGateDetector{}.Detect(dir)
	if len(gates) != 1 {
		t.Fatalf("got %d gates, want 1", len(gates))
	}
	if gates[0].Name != "rust" {
		t.Errorf("gate name = %q, want rust", gates[0].Name)
	}
}

func TestDetectMakefileProject(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte("test:"), 0644); err != nil {
		t.Fatal(err)
	}

	gates := engine.FileGateDetector{}.Detect(dir)
	if len(gates) != 1 {
		t.Fatalf("got %d gates, want 1", len(gates))
	}
	if gates[0].Name != "make" {
		t.Errorf("gate name = %q, want make", gates[0].Name)
	}
}

func TestDetectMultiProjectOrder(t *testing.T) {
	dir := t.TempDir()
	// Go + Makefile
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x"), 0644)
	os.WriteFile(filepath.Join(dir, "Makefile"), []byte("test:"), 0644)

	gates := engine.FileGateDetector{}.Detect(dir)
	if len(gates) != 2 {
		t.Fatalf("got %d gates, want 2", len(gates))
	}
	// Deterministic order: Go before Make.
	if gates[0].Name != "go" {
		t.Errorf("gate[0] = %q, want go", gates[0].Name)
	}
	if gates[1].Name != "make" {
		t.Errorf("gate[1] = %q, want make", gates[1].Name)
	}
}

func TestDetectEmptyDir(t *testing.T) {
	dir := t.TempDir()
	gates := engine.FileGateDetector{}.Detect(dir)
	if len(gates) != 0 {
		t.Errorf("got %d gates from empty dir, want 0", len(gates))
	}
}

// --- Fake ports for ChapterVerifier tests ---

type fakeRunner struct {
	allPass bool
}

func (f *fakeRunner) RunGates(_ string, gates []engine.QualityGate) []engine.GateResult {
	results := make([]engine.GateResult, len(gates))
	for i, g := range gates {
		results[i] = engine.GateResult{
			Gate:   g,
			Passed: f.allPass,
			Output: "test output",
		}
	}
	return results
}

type fakeCheckpointer struct {
	hasChanges bool
	commitHash string
	committed  bool
	rolledBack bool
}

func (f *fakeCheckpointer) CommitChapter(_ string, _ int, _ string, _ *engine.ChapterMark) (string, error) {
	f.committed = true
	return f.commitHash, nil
}

func (f *fakeCheckpointer) MarkChapter(_ string) (engine.ChapterMark, error) {
	return engine.ChapterMark{}, nil
}

func (f *fakeCheckpointer) RollbackChapter(_ string, _ *engine.ChapterMark) error {
	f.rolledBack = true
	return nil
}

func (f *fakeCheckpointer) HasChanges(_ string, _ *engine.ChapterMark) (bool, error) {
	return f.hasChanges, nil
}

type fakeDetector struct {
	gates []engine.QualityGate
}

func (f *fakeDetector) Detect(_ string) []engine.QualityGate {
	return f.gates
}

// --- ChapterVerifier tests ---

func TestVerifyCommitsOnGreen(t *testing.T) {
	cp := &fakeCheckpointer{hasChanges: true, commitHash: "abc1234"}
	cv := &engine.ChapterVerifier{
		Detector:     &fakeDetector{gates: []engine.QualityGate{{Name: "go", Command: "go test ./..."}}},
		Runner:       &fakeRunner{allPass: true},
		Checkpointer: cp,
	}

	ch := engine.Chapter{Number: 1, Title: "add feature"}
	result, err := cv.Verify(context.Background(), "/repo", ch)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !result.Passed {
		t.Error("expected Passed=true")
	}
	if result.Commit != "abc1234" {
		t.Errorf("Commit = %q, want abc1234", result.Commit)
	}
	if !cp.committed {
		t.Error("expected CommitChapter to be called")
	}
	if cp.rolledBack {
		t.Error("did not expect RollbackChapter to be called")
	}
}

func TestVerifyRollsBackOnFail(t *testing.T) {
	cp := &fakeCheckpointer{hasChanges: true, commitHash: "abc1234"}
	cv := &engine.ChapterVerifier{
		Detector:     &fakeDetector{gates: []engine.QualityGate{{Name: "go", Command: "go test ./..."}}},
		Runner:       &fakeRunner{allPass: false},
		Checkpointer: cp,
	}

	ch := engine.Chapter{Number: 2, Title: "break things"}
	result, err := cv.Verify(context.Background(), "/repo", ch)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if result.Passed {
		t.Error("expected Passed=false")
	}
	if result.Strikes != 1 {
		t.Errorf("Strikes = %d, want 1", result.Strikes)
	}
	if cp.committed {
		t.Error("did not expect CommitChapter to be called")
	}
	if !cp.rolledBack {
		t.Error("expected RollbackChapter to be called")
	}
}

func TestVerifyNoChangesSkips(t *testing.T) {
	cp := &fakeCheckpointer{hasChanges: false}
	cv := &engine.ChapterVerifier{
		Detector:     &fakeDetector{gates: []engine.QualityGate{{Name: "go", Command: "go test ./..."}}},
		Runner:       &fakeRunner{allPass: false}, // runner should not be called
		Checkpointer: cp,
	}

	ch := engine.Chapter{Number: 3, Title: "read-only audit"}
	result, err := cv.Verify(context.Background(), "/repo", ch)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !result.Passed {
		t.Error("expected Passed=true for no-changes chapter")
	}
	if cp.committed {
		t.Error("did not expect commit for no-changes chapter")
	}
}

func TestVerifyNoGatesCommitsUnconditionally(t *testing.T) {
	cp := &fakeCheckpointer{hasChanges: true, commitHash: "def5678"}
	cv := &engine.ChapterVerifier{
		Detector:     &fakeDetector{gates: nil}, // no gates
		Runner:       &fakeRunner{allPass: false},
		Checkpointer: cp,
	}

	ch := engine.Chapter{Number: 4, Title: "add docs"}
	result, err := cv.Verify(context.Background(), "/repo", ch)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !result.Passed {
		t.Error("expected Passed=true with no gates")
	}
	if result.Commit != "def5678" {
		t.Errorf("Commit = %q, want def5678", result.Commit)
	}
	if !cp.committed {
		t.Error("expected CommitChapter to be called")
	}
}
