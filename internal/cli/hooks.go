package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/hooks"
	"github.com/onembyte/kolkrabbi/internal/shell"
	"github.com/onembyte/kolkrabbi/internal/tools"
)

// newHookRunner builds the runner for this session from the user's own hooks
// file, or returns nil when there is none.
//
// **The user's file only.** A `.kolk/hooks.json` in a cloned repository is a
// shell command a stranger wrote, and reading one is G16.3's job: it has to be
// shown and confirmed before the first one runs, because cloning a repository
// must not be enough to execute anything. This leaf deliberately stops at the
// file the person wrote themselves.
func (a *app) newHookRunner(sessionID, effort string) *hooks.Runner {
	d, err := a.resolve()
	if err != nil {
		return nil
	}
	raw, err := os.ReadFile(filepath.Join(d.Config, "hooks.json"))
	if err != nil {
		return nil
	}
	var file struct {
		Hooks hooks.Config `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &file.Hooks); err != nil || len(file.Hooks.PostEdit)+len(file.Hooks.PostWrite)+len(file.Hooks.SessionEnd) == 0 {
		// A malformed hooks file costs its hooks, not the session. Someone
		// mid-edit on their own config should still be able to work.
		return nil
	}

	return &hooks.Runner{
		Shell:   shell.New(),
		Config:  file.Hooks,
		Session: sessionID,
		// Bounded like bash, by the same dial, because a hook is a command and
		// a hook that hangs must not hang the session.
		Timeout: engine.TimeoutForEffort(effort),
		Allowed: func(command string) bool {
			verdict, _ := engine.PermissionAsk.Judge(tools.Request{Tool: "bash", Command: command})
			return verdict != engine.VerdictDeny
		},
		Confirm: func(command string) bool {
			fmt.Fprintf(a.stdout, "\nA hook wants to run: %s\n", command)
			return a.confirmHook(command)
		},
	}
}

// confirmHook asks once per distinct command. The prompt is deliberately the
// plainest possible: a hook is a shell command, and the question is whether
// this session may run it.
func (a *app) confirmHook(command string) bool {
	if a.in == nil {
		// Nobody is there to answer, and a hook that runs because no one said
		// no is the silent shell command this whole leaf exists to prevent.
		return false
	}
	fmt.Fprint(a.stdout, "run it this session? [y/N] ")
	line, err := a.in.ReadString('\n')
	if err != nil {
		return false
	}
	answer := strings.TrimSpace(line)
	return answer == "y" || answer == "Y"
}
