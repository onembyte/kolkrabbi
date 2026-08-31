package engine

import (
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/secret"
	"github.com/onembyte/kolkrabbi/protocol"
)

// subagentProviderProgress converts one provider-owned boundary into the
// latest observed step of exactly one child task. Provider tool execution is
// never re-dispatched through Kolkrabbi's local tool executor.
func (a *Agent) subagentProviderProgress(index int) func(provider.ProgressEvent) {
	return func(event provider.ProgressEvent) {
		if step := providerProgressStep(event); step != "" {
			a.updateSubagentStatus(index, SubagentWorking, SubagentPhaseProvider, step)
		}
	}
}

// mainProviderProgress records provider-owned boundaries for the parent turn.
// It deliberately never carries task coordinates: a main model call may plan,
// synthesize, or directly execute one task, but it is still parent work.
func (a *Agent) mainProviderProgress(model, effort string) func(provider.ProgressEvent) {
	return func(event provider.ProgressEvent) {
		if step := providerProgressStep(event); step != "" {
			a.publishMainWork(protocol.WorkStateWorking, protocol.WorkPhaseProvider, step, model, effort)
		}
	}
}

func providerProgressStep(event provider.ProgressEvent) string {
	detail := compactSubagentStep(secret.Scrub(event.Detail))
	name := compactSubagentStep(secret.Scrub(event.Name))
	safe := func(step string) string { return compactSubagentStep(step) }
	switch event.Kind {
	case provider.ProgressMessage:
		return safe("model is responding")
	case provider.ProgressToolStarted:
		if name == "" {
			name = "tool"
		}
		return safe("provider tool " + name + " started")
	case provider.ProgressToolFinished:
		if name == "" {
			name = "tool"
		}
		if event.Error {
			if detail != "" {
				return safe("provider tool " + name + " failed: " + detail)
			}
			return safe("provider tool " + name + " failed")
		}
		return safe("provider tool " + name + " finished")
	case provider.ProgressError:
		if detail != "" {
			return safe("provider error: " + detail)
		}
		return safe("provider error")
	case provider.ProgressLimit:
		prefix := "provider plan limit"
		if event.Error {
			prefix += " reached"
		}
		if detail != "" {
			return safe(prefix + ": " + detail)
		}
		return safe(prefix)
	default:
		return ""
	}
}
