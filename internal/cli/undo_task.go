package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/checkpoint"
	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/provider"
)

// undoTask takes back one writing subagent's file changes.
//
// It deliberately does not trim the conversation the way `/undo` does. A turn
// that ran five subagents is one turn: taking back its history to undo the
// second would take the other four with it. But files and history still have to
// stay in step — the divergence `/undo` exists to prevent — so the model is
// told, in the transcript, exactly what went back.
func (a *app) undoTask(ctx context.Context, ag *engine.Agent, arg string) {
	store, ok := ag.Ckpt.(*checkpoint.Store)
	if !ok || ag.Ckpt == nil {
		fmt.Fprintln(a.stdout, "checkpointing is not enabled, so there is no subagent to take back.")
		return
	}

	snapshots := store.TaskSnapshots()
	if arg == "" {
		if len(snapshots) == 0 {
			fmt.Fprintln(a.stdout, "no subagent in this session has a snapshot to take back.")
			return
		}
		fmt.Fprintln(a.stdout, "subagents that can be taken back on their own:")
		for i, snapshot := range snapshots {
			fmt.Fprintf(a.stdout, "  %d. %s (%d file(s))\n", i+1, snapshot.Title, len(snapshot.Paths))
		}
		fmt.Fprintln(a.stdout, "`/undo task <n>` takes one back and leaves the rest of the turn alone.")
		return
	}

	n, err := strconv.Atoi(arg)
	if err != nil {
		fmt.Fprintf(a.stderr, "`/undo task <n>` takes a subagent number, not %q.\n", arg)
		return
	}

	restored, err := store.RewindTask(ctx, n)
	if err != nil {
		fmt.Fprintf(a.stderr, "undo failed: %v\n", err)
		return
	}
	title := ""
	if n >= 1 && n <= len(snapshots) {
		title = snapshots[n-1].Title
	}
	if len(restored) == 0 {
		fmt.Fprintf(a.stdout, "subagent %d (%s) changed no files; nothing to take back.\n", n, title)
		return
	}
	fmt.Fprintf(a.stdout, "took back subagent %d (%s), %d file(s) restored:\n", n, title, len(restored))
	for _, path := range restored {
		fmt.Fprintln(a.stdout, "  "+path)
	}

	// Without this the model goes on believing edits that are no longer on
	// disk are still there, and reasons from them next turn.
	if ag.Sess != nil {
		ag.Sess.AppendMessage(provider.Message{
			Role: "user",
			Content: fmt.Sprintf("I took back subagent %d (%s) with `/undo task %d`. Its changes to %s are gone; "+
				"the rest of the turn stands. Do not assume those edits are still on disk.",
				n, title, n, strings.Join(restored, ", ")),
		})
	}
}
