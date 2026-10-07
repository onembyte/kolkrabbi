package engine

import (
	"context"

	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/secret"
)

// WorkRecord is an observed tool outcome for the interactive work log. It is
// separate from model messages: display excerpts never replace tool results.
type WorkRecord struct {
	Agent                              int
	Name, Arguments, Output, Error     string
	Path, Diff                         string
	Added, Removed                     int
	Changed, Created, Failed, Provider bool
	Pending, Warning                   bool
}

type workAgentKey struct{}

func (a *Agent) emitWorkRecord(record WorkRecord) {
	if a.WorkLog == nil {
		return
	}
	record.Name = secret.Scrub(record.Name)
	record.Arguments = secret.Scrub(record.Arguments)
	record.Output = secret.Scrub(record.Output)
	record.Error = secret.Scrub(record.Error)
	record.Path = secret.Scrub(record.Path)
	record.Diff = secret.Scrub(record.Diff)
	a.WorkLog(record)
}

func workAgent(ctx context.Context) int {
	index, _ := ctx.Value(workAgentKey{}).(int)
	return index
}

// Each provider stream owns its pending map. Parallel children never match a
// completion to another child's identically named tool call.
func (a *Agent) providerWorkLog(agent int) func(provider.ProgressEvent) {
	pending := make(map[string]provider.ProgressEvent)
	var order []string
	var anonymous []provider.ProgressEvent
	return func(event provider.ProgressEvent) {
		if a.WorkLog == nil {
			return
		}
		switch event.Kind {
		case provider.ProgressToolStarted:
			if event.ID != "" {
				if _, exists := pending[event.ID]; !exists {
					order = append(order, event.ID)
				}
				pending[event.ID] = event
			} else {
				anonymous = append(anonymous, event)
			}
		case provider.ProgressToolFinished:
			start, exists := pending[event.ID]
			if !exists {
				return
			}
			delete(pending, event.ID)
			output := event.Output
			if output == "" {
				output = event.Detail
			}
			a.emitWorkRecord(WorkRecord{Agent: agent, Name: start.Name, Arguments: start.Input,
				Output: output, Failed: event.Error, Provider: true})
		case provider.ProgressWarning:
			a.emitWorkRecord(WorkRecord{Agent: agent, Name: event.Name, Output: event.Input, Warning: true, Provider: true})
		case provider.ProgressStreamEnded:
			for _, id := range order {
				if start, exists := pending[id]; exists {
					a.emitWorkRecord(WorkRecord{Agent: agent, Name: start.Name, Arguments: start.Input,
						Output: "No completion reported", Pending: true, Provider: true})
					delete(pending, id)
				}
			}
			for _, start := range anonymous {
				a.emitWorkRecord(WorkRecord{Agent: agent, Name: start.Name, Arguments: start.Input,
					Output: "No completion reported", Pending: true, Provider: true})
			}
			order, anonymous = nil, nil
		}
	}
}
