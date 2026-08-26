package engine

import "os"

// FileGateDetector discovers quality gates by probing for well-known project
// files. It fulfils the QualityGateDetector port using only os.Stat, keeping
// the engine free of shell or exec dependencies.
type FileGateDetector struct{}

// Detect returns the quality gates that apply to repoDir, in a fixed
// deterministic order: Go → Node → Rust → Make.
func (FileGateDetector) Detect(repoDir string) []QualityGate {
	var gates []QualityGate

	if fileExists(repoDir, "go.mod") {
		gates = append(gates, QualityGate{
			Name:    "go",
			Command: "go vet ./... && go test ./...",
		})
	}
	if fileExists(repoDir, "package.json") {
		gates = append(gates, QualityGate{
			Name:    "node",
			Command: "npm test",
		})
	}
	if fileExists(repoDir, "Cargo.toml") {
		gates = append(gates, QualityGate{
			Name:    "rust",
			Command: "cargo test",
		})
	}
	// Makefile gates: prefer "make check" if a check target exists,
	// otherwise fall back to "make test".
	if fileExists(repoDir, "Makefile") || fileExists(repoDir, "makefile") || fileExists(repoDir, "GNUmakefile") {
		gates = append(gates, QualityGate{
			Name:    "make",
			Command: "make test",
		})
	}

	return gates
}

func fileExists(dir, name string) bool {
	_, err := os.Stat(dir + "/" + name)
	return err == nil
}
