package local

import (
	"os"
	"path/filepath"
	"testing"
)

// PulledNames reads the manifest tree the sidecar writes when a pull lands. A
// directory kolk has never created is "nothing pulled yet" — the picker has to
// draw the same way on a fresh machine.
func TestPulledNamesReadsTheSidecarManifestTree(t *testing.T) {
	modelDir := t.TempDir()
	if pulled := PulledNames(modelDir); len(pulled) != 0 {
		t.Fatalf("empty model dir → %+v, want nothing pulled", pulled)
	}

	library := filepath.Join(modelDir, "manifests", "registry.ollama.ai", "library")
	for _, model := range []struct{ name, tag string }{
		{"qwen2.5-coder", "7b"},
		{"llama3.1", "8b"},
	} {
		if err := os.MkdirAll(filepath.Join(library, model.name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(library, model.name, model.tag), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A library directory with no tags under it is an interrupted pull, not a
	// model anyone can run.
	if err := os.MkdirAll(filepath.Join(library, "phi4"), 0o755); err != nil {
		t.Fatal(err)
	}

	pulled := PulledNames(modelDir)
	if len(pulled) != 2 || !pulled["qwen2.5-coder"] || !pulled["llama3.1"] {
		t.Fatalf("pulled = %+v, want exactly the two completed pulls", pulled)
	}
	if PulledName(pulled, "qwen2.5-coder:7b") != true {
		t.Fatal("catalog's tagged name did not map onto its library manifest")
	}
	if PulledName(pulled, "phi4:14b") || PulledName(pulled, "gemma2:9b") {
		t.Fatal("an unpulled or interrupted pull reads as present")
	}
}
