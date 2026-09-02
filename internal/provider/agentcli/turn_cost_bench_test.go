package agentcli

import (
	"os"
	"path/filepath"
	"testing"
)

// What one turn costs before the model is even asked.
//
// The harness's own overhead is nothing against a model call, but it is paid
// per turn and per subagent, and a saga is hundreds of turns. These measure the
// work that repeats: validating the execution envelope (EvalSymlinks + Stat for
// the workspace and every additional directory) and rebuilding argv.
func benchWorkspace(b *testing.B, additional int) ExecutionOptions {
	b.Helper()
	options := ExecutionOptions{Workspace: b.TempDir()}
	for i := 0; i < additional; i++ {
		dir := filepath.Join(b.TempDir(), "extra")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			b.Fatal(err)
		}
		options.AdditionalDirs = append(options.AdditionalDirs, dir)
	}
	return options
}

// Codex opens one process per turn, so this is paid on every turn.
//
// Two shapes, because they measure different moments. `/raw` is a first call:
// an envelope nobody has validated, which is what a constructor pays once.
// `/perturn` is what the loop actually pays — the envelope the backend already
// holds, validated at construction. Before the normalized marker existed the
// two were the same number, because every turn re-validated.
func BenchmarkCodexTurnArgv(b *testing.B) {
	raw := benchWorkspace(b, 2)
	perTurn, err := normalizeExecutionOptions(raw)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("raw", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := BuildCodexInvocationWithOptions("gpt-5.6-sol", "code", "high", "", false, "do the thing", raw); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("perturn", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := BuildCodexInvocationWithOptions("gpt-5.6-sol", "code", "high", "", false, "do the thing", perTurn); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// Claude keeps one process for the whole session, and builds this invocation on
// every turn anyway — the persistent path returns before it is ever used.
func BenchmarkClaudeTurnInvocation(b *testing.B) {
	options := benchWorkspace(b, 2)
	// Claude has no network switch, so a delegated envelope declares it (F2).
	options.NetworkAccess = true
	b.ReportAllocs()
	for b.Loop() {
		if _, err := BuildClaudeInvocationWithOptions("claude-fable", "code", "high", "do the thing", options); err != nil {
			b.Fatal(err)
		}
	}
}

// The envelope validation alone, which both paths pay and the shell then pays
// again when it normalises the process working directory.
func BenchmarkExecutionEnvelopeValidation(b *testing.B) {
	raw := benchWorkspace(b, 2)
	normalized, err := normalizeExecutionOptions(raw)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("raw", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := normalizeExecutionOptions(raw); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("alreadyvalidated", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := normalizeExecutionOptions(normalized); err != nil {
				b.Fatal(err)
			}
		}
	})
}
