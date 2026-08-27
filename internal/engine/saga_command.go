package engine

import "context"

// CommandResult is the engine-facing result of a project command.
type CommandResult struct {
	Output   string
	ExitCode int
	Failure  string
}

// CommandRunner is implemented by the platform shell adapter. Keeping this
// port small prevents saga orchestration from importing the platform layer.
type CommandRunner interface {
	Run(context.Context, string, string) (CommandResult, error)
}
