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
		// No file of the user's own is not the end of it: a project may still
		// declare hooks, and those are exactly the ones worth asking about.
		raw = []byte("{}")
	}
	var file struct {
		Hooks hooks.Config `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &file.Hooks); err != nil || len(file.Hooks.PostEdit)+len(file.Hooks.PostWrite)+len(file.Hooks.SessionEnd) == 0 {
		// A malformed hooks file costs its hooks, not the session. Someone
		// mid-edit on their own config should still be able to work.
		return nil
	}

	config := file.Hooks
	// The project's hooks are read, shown in full, and confirmed once — before
	// any of them runs. Declining leaves the user's own hooks working.
	if project, found := hooks.LoadProject(projectRoot()); found {
		if a.approveProjectHooks(project) {
			config = hooks.Merge(config, project.Config)
		}
	}

	return &hooks.Runner{
		Shell:   shell.New(),
		Config:  config,
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

// approveProjectHooks shows a repository's hooks and asks once.
//
// **All of them, together, before any of them runs.** Cloning a repository must
// not be enough to execute anything, and showing them one at a time as each
// event fired would let a repository hide the fifth behind four boring ones.
//
// The answer is remembered for **this session only**, keyed by the file's
// fingerprint. Two reasons, and the second is the interesting one. A remembered
// yes that survives restarts is a thing a repository can farm — approve
// something harmless, edit it later, and the approval outlives the thing it was
// given for. And keying on content rather than path means an edit re-asks even
// within one session, so approval attaches to what was actually read.
//
// Persistence is refused for now rather than deferred vaguely: it would need a
// store the project cannot influence and an expiry nobody has yet felt the need
// for. If being asked once per session turns out to be the friction that makes
// people stop reading the list, that is the moment to add it — and the list
// being read is the entire point.
func (a *app) approveProjectHooks(project hooks.Project) bool {
	if decided, answered := a.projectHooksApproved[project.Fingerprint]; answered {
		return decided
	}
	if a.projectHooksApproved == nil {
		a.projectHooksApproved = map[string]bool{}
	}

	fmt.Fprintf(a.stdout, "\nThis project declares hooks in %s:\n", project.Path)
	for _, command := range project.Commands() {
		fmt.Fprintf(a.stdout, "  %s\n", command)
	}
	fmt.Fprintln(a.stdout, "These are shell commands from this repository, not from you.")
	fmt.Fprint(a.stdout, "allow them this session? [y/N] ")

	allowed := false
	if a.in != nil {
		if line, err := a.in.ReadString('\n'); err == nil {
			answer := strings.TrimSpace(line)
			allowed = answer == "y" || answer == "Y"
		}
	}
	if !allowed {
		fmt.Fprintln(a.stdout, "not running them. your own hooks are unaffected.")
	}
	a.projectHooksApproved[project.Fingerprint] = allowed
	return allowed
}
