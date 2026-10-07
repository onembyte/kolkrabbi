package engine

import (
	"fmt"
	"io"

	"github.com/onembyte/kolkrabbi/internal/provider"
)

func (a *Agent) childWindow(pinned pinnedBackend, model string) int {
	backend, wire, _ := a.backendFor(model)
	if own := pinned.forModel(model); own != nil {
		backend = own
	}
	if reporter, ok := backend.(windowReporter); ok {
		if size := reporter.ContextWindow(wire); size > 0 {
			return size
		}
	}
	for _, info := range a.Catalog {
		if info.ID == model {
			return info.ContextLength
		}
	}
	return 0 // The parent's advertised window does not describe a smaller child.
}

func (a *Agent) orchestrationWindow(model string) int {
	if model == a.SessionModel() {
		return a.window()
	}
	return a.childWindow(pinnedBackend{}, model)
}

func (a *Agent) compactChild(index int, messages []provider.Message, target int, out io.Writer) ([]provider.Message, bool) {
	result, err := CompactMessages(messages, keepRecentTurns, target, nil)
	if err != nil || result.FreedTokens <= 0 || result.Replaced == 0 {
		return messages, false
	}
	return a.retainChildCompaction(index, messages, result, out)
}

func (a *Agent) retainChildCompaction(index int, messages []provider.Message, result Compaction, out io.Writer) ([]provider.Message, bool) {
	path, err := a.archiveMessages(messages)
	if err != nil {
		fmt.Fprintf(out, "could not compact agent %d: full history could not be archived: %v\n", index+1, err)
		return messages, false
	}
	a.executionMu.Lock()
	if a.execution != nil && index >= 0 && index < len(a.execution.Tasks) {
		task := &a.execution.Tasks[index]
		task.Archives = append(task.Archives, path)
	}
	a.executionMu.Unlock()
	// Save the archive reference and original working messages before reducing
	// the worker's private context. A failed save retains the complete context.
	a.storeExecution()
	if a.Sess != nil {
		if err := a.Sess.Save(); err != nil {
			fmt.Fprintf(out, "could not compact agent %d: session could not be saved: %v\n", index+1, err)
			return messages, false
		}
	}
	fmt.Fprintf(out, "compacted agent %d context; freed about %d tokens · full history: %s\n", index+1, result.FreedTokens, path)
	return result.Messages, true
}
