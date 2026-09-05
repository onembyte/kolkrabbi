package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/onembyte/kolkrabbi/internal/engine"
	"github.com/onembyte/kolkrabbi/internal/paths"
	"github.com/onembyte/kolkrabbi/internal/shell"
)

// sandboxPolicyFor is the one place a Sandbox is built from a project root, so
// the session switch and the config-at-startup path cannot disagree about what
// "on" means. Root is the jail root; Temp is the process temp dir; the denylist
// is plan 13 §3's hardline paths; network stays allowed for the user's own
// commands, by the owner's decision -- the sandbox confines writes, it does
// not cut the internet.
func sandboxPolicyFor(root string, d paths.Dirs) *shell.Sandbox {
	// paths owns the home-directory lookup (arch: osOwner); everything else
	// asks it, so "the engine touches no OS" stays a property, not a habit.
	home, _ := paths.UserHomeDir()
	return &shell.Sandbox{
		Root:     root,
		Temp:     os.TempDir(),
		Writable: toolchainCaches(home),
		Deny:     shell.CredentialDenylist(home, d.CredentialsFile()),
		Network:  shell.NetworkAllow,
	}
}

// showSandbox prints the state and the one command that changes it.
func (a *app) showSandbox(ag *engine.Agent) {
	if ag.Sandbox == nil {
		fmt.Fprintln(a.stdout, "sandbox: off — bash commands run unconfined (the default)")
		fmt.Fprintln(a.stdout, "turn it on for this session: /sandbox on   · persist it: /config set sandbox on")
		return
	}
	name, err := shell.Mechanism()
	if err != nil {
		// On, and nothing can enforce it: every command will refuse. Say so
		// here rather than letting the user discover it one command at a time.
		fmt.Fprintf(a.stdout, "sandbox: on, but %v — commands will refuse until /sandbox off\n", err)
		return
	}
	fmt.Fprintf(a.stdout, "sandbox: on (%s) — writes confined to %s and the temp dir; network allowed\n", name, ag.Sandbox.Root)
	fmt.Fprintln(a.stdout, "turn it off for this session: /sandbox off")
}

// setSandbox handles `/sandbox [on|off]`. Turning it on where nothing can
// enforce it is refused at the ask, with the reason, and toggles nothing:
// fail closed, at the moment somebody can still do something about it.
func (a *app) setSandbox(ag *engine.Agent, arg string) {
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "":
		a.showSandbox(ag)
	case "on":
		name, err := shell.Mechanism()
		if err != nil {
			fmt.Fprintf(a.stdout, "cannot enable the sandbox: %v\n", err)
			return
		}
		d, _ := a.locate()
		ag.SetSandbox(sandboxPolicyFor(ag.Root, d))
		fmt.Fprintf(a.stdout, "sandbox → on (%s) — writes confined to %s and the temp dir; network allowed\n", name, ag.Root)
	case "off":
		ag.SetSandbox(nil)
		fmt.Fprintln(a.stdout, "sandbox → off — bash commands run unconfined")
	default:
		fmt.Fprintf(a.stdout, "%q is not a sandbox setting; use /sandbox on or /sandbox off\n", strings.TrimSpace(arg))
	}
}

// sandboxFromConfig turns the saved setting into a policy, or nil. Only the
// literal "on" counts; "off" and unset are both off.
func sandboxFromConfig(setting, root string, d paths.Dirs) *shell.Sandbox {
	if strings.EqualFold(strings.TrimSpace(setting), "on") {
		return sandboxPolicyFor(root, d)
	}
	return nil
}

// toolchainCaches are the directories a build writes to that are not the
// project: the user cache dir (go-build, pip, npm), GOPATH and GOMODCACHE. The
// environment is honoured when it is set and Go's defaults are used when it is
// not, because that is exactly what the toolchain itself will do.
func toolchainCaches(home string) []string {
	var out []string
	if cache, err := paths.UserCacheDir(); err == nil && cache != "" {
		out = append(out, cache)
	}
	gopath := os.Getenv("GOPATH")
	if gopath == "" && home != "" {
		gopath = home + "/go"
	}
	if gopath != "" {
		out = append(out, gopath)
	}
	if mod := os.Getenv("GOMODCACHE"); mod != "" {
		out = append(out, mod)
	}
	return out
}
