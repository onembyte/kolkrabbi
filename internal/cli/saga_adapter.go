package cli

import (
	"context"
	"fmt"

	"github.com/onembyte/kolkrabbi/internal/atomicfile"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/shell"
)

type sagaCommandRunner struct{ shell shell.Shell }

func (r sagaCommandRunner) Run(ctx context.Context, command, dir string) (engine.CommandResult, error) {
	result, err := r.shell.Run(ctx, shell.Cmd{Command: command, Dir: dir})
	return engine.CommandResult{
		Output:   result.Output,
		ExitCode: result.ExitCode,
		Failure:  result.Failure,
	}, err
}

// VerifySagaChapter is the CLI boundary for the engine's platform-neutral
// saga lifecycle. It supplies the real shell and durable atomic artifact
// writer while leaving lifecycle policy in engine.
func VerifySagaChapter(ctx context.Context, sh shell.Shell, repoDir string, state *engine.SagaState, chapterIndex int) error {
	if sh == nil {
		return fmt.Errorf("saga: shell is required")
	}
	runner := sagaCommandRunner{shell: sh}
	verifier := &engine.ChapterVerifier{
		Detector:     engine.FileGateDetector{},
		Runner:       engine.NewCommandGateRunner(ctx, runner),
		Checkpointer: engine.NewCommandCheckpointer(ctx, runner),
	}
	return engine.VerifyChapterAndPersist(ctx, verifier, repoDir, state, chapterIndex, atomicfile.Write)
}
