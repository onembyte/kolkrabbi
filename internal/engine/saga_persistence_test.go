package engine_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/atomicfile"
	"github.com/onembyte/kolkrabbi/internal/engine"
)

func TestSaveSagaArtifactUsesRepositorySAGAPathAndMode(t *testing.T) {
	var gotPath string
	var gotData []byte
	var gotMode os.FileMode
	err := engine.SaveSagaArtifact("/repo", &engine.SagaState{Goal: "ship"},
		func(path string, data []byte, mode os.FileMode) error {
			gotPath, gotData, gotMode = path, data, mode
			return nil
		})
	if err != nil {
		t.Fatalf("SaveSagaArtifact() error = %v", err)
	}
	if gotPath != filepath.Join("/repo", "SAGA.md") {
		t.Fatalf("path = %q", gotPath)
	}
	if string(gotData) == "" || gotMode != 0o600 {
		t.Fatalf("artifact data/mode = %q/%o", gotData, gotMode)
	}
}

func TestSaveSagaArtifactWithAtomicWriter(t *testing.T) {
	dir := t.TempDir()
	if err := engine.SaveSagaArtifact(dir, &engine.SagaState{Goal: "durable"}, atomicfile.Write); err != nil {
		t.Fatalf("SaveSagaArtifact() error = %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "SAGA.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) == "" || string(body)[0] != '#' {
		t.Fatalf("unexpected artifact: %q", body)
	}
}

func TestSaveSagaArtifactSurfacesWriterFailure(t *testing.T) {
	want := os.ErrPermission
	err := engine.SaveSagaArtifact("/repo", &engine.SagaState{},
		func(string, []byte, os.FileMode) error { return want })
	if !os.IsPermission(err) {
		t.Fatalf("error = %v, want permission error", err)
	}
}
