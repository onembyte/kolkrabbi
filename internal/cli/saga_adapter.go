package cli

import (
	"context"

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
