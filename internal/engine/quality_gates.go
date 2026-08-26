package engine

import (
	"os"
	"path/filepath"
	"strings"
)

// DetectQualityGates returns the project verification commands implied by the
// repository files in repoDir. The order is stable and follows the project's
// primary toolchain before an optional Makefile gate.
func DetectQualityGates(repoDir string) []string {
	if strings.TrimSpace(repoDir) == "" {
		return nil
	}

	var gates []string
	if exists(filepath.Join(repoDir, "go.mod")) {
		gates = append(gates, "go vet ./... && go test ./...")
	}
	if exists(filepath.Join(repoDir, "package.json")) {
		gates = append(gates, "npm test")
	}
	if exists(filepath.Join(repoDir, "Cargo.toml")) {
		gates = append(gates, "cargo test")
	}
	if makefileHasTarget(filepath.Join(repoDir, "Makefile"), "check") {
		gates = append(gates, "make check")
	} else if makefileHasTarget(filepath.Join(repoDir, "Makefile"), "test") {
		gates = append(gates, "make test")
	}
	return gates
}

func exists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func makefileHasTarget(path, target string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	prefix := target + ":"
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}
