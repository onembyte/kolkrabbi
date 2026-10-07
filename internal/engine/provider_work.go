package engine

import (
	"github.com/onembyte/kolkrabbi/internal/continuity"
	"github.com/onembyte/kolkrabbi/internal/provider"
	"github.com/onembyte/kolkrabbi/internal/secret"
	"github.com/onembyte/kolkrabbi/protocol"
)

// subagentProviderProgress converts one provider-owned boundary into the
// latest observed step of exactly one child task. Provider tool execution is
// never re-dispatched through Kolkrabbi's local tool executor.
func (a *Agent) subagentProviderProgress(index int) func(provider.ProgressEvent) {
	log := a.providerWorkLog(index + 1)
	return func(event provider.ProgressEvent) {
		log(event)
		a.journalVendorTool(index, event)
		if step := providerProgressStep(event); step != "" {
			a.updateSubagentStatus(index, SubagentWorking, SubagentPhaseProvider, step)
		}
	}
}

// mainProviderProgress records provider-owned boundaries for the parent turn.
// It deliberately never carries task coordinates: a main model call may plan,
// synthesize, or directly execute one task, but it is still parent work.
func (a *Agent) mainProviderProgress(model, effort string) func(provider.ProgressEvent) {
	log := a.providerWorkLog(0)
	return func(event provider.ProgressEvent) {
		log(event)
		a.journalVendorTool(-1, event)
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

// journalVendorTool keeps what a provider-owned turn says about its own tools
// in the run journal, since only its live stream says it: the tools started
// and not yet finished, and the last one finished. index -1 is the main
// session. Resume reads it to know which vendor actions completed.
func (a *Agent) journalVendorTool(index int, event provider.ProgressEvent) {
	if event.Kind != provider.ProgressToolStarted && event.Kind != provider.ProgressToolFinished || event.ID == "" {
		return
	}
	a.executionMu.Lock()
	defer a.executionMu.Unlock()
	if a.execution == nil || index >= len(a.execution.Tasks) {
		return
	}
	task := &a.execution.Main
	if index >= 0 {
		task = &a.execution.Tasks[index]
	}
	if task.VendorTools == nil {
		task.VendorTools = &continuity.VendorToolBoundary{}
	}
	tools := task.VendorTools
	unfinished := tools.Unfinished[:0:0]
	for _, id := range tools.Unfinished {
		if id != event.ID {
			unfinished = append(unfinished, id)
		}
	}
	if event.Kind == provider.ProgressToolStarted {
		unfinished = append(unfinished, event.ID)
	} else {
		tools.LastFinishedID, tools.LastFinishedName, tools.LastFinishedFailed = event.ID, secret.Scrub(event.Name), event.Error
	}
	tools.Unfinished = unfinished
}
