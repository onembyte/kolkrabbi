package engine

import (
	"fmt"
	"os"
	"path/filepath"
)

// ArtifactWriter persists one complete artifact. Callers should provide the
// repository's atomic writer; the engine only owns serialization and naming.
type ArtifactWriter func(path string, data []byte, perm os.FileMode) error

// SaveSagaArtifact atomically persists the human-readable saga state at the
// repository root.
func SaveSagaArtifact(repoDir string, state *SagaState, write ArtifactWriter) error {
	if state == nil {
		return fmt.Errorf("saga: state is required")
	}
	if write == nil {
		return fmt.Errorf("saga: artifact writer is required")
	}
	if repoDir == "" {
		return fmt.Errorf("saga: repository directory is required")
	}
	return write(filepath.Join(repoDir, "SAGA.md"), []byte(FormatSagaMarkdown(state)), 0o600)
}
