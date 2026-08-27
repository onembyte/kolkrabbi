package engine

import "os"

// UNREACHABLE as of 2026-08-27, found by a checkpoint audit. Nothing outside
// this package's own tests refers to it, and `internal/` means nothing outside
// the module can. The saga's live path is DetectQualityGates in
// quality_gates.go, called from internal/cli/saga_adapter.go, which does the
// same job with a different shape.
//
// Kept rather than deleted because this is the better design of the two — it
// depends only on ports and never on shell — so the choice is whether to wire
// it or drop it, and that is worth deciding rather than defaulting. Its tests
// pass, which is exactly why the duplication survived: green tests read as
// live code.
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
