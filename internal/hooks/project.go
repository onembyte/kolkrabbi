package hooks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
)

// Project is a repository's hooks file, and what it would run.
type Project struct {
	Config Config
	Path   string
	// Fingerprint is a hash of the file's contents.
	//
	// Approval is keyed by what the file **says**, not by where it lives. A
	// repository whose hooks change after you approved them must ask again —
	// otherwise "yes" is a thing a stranger can later edit into meaning
	// something else, which is the whole hazard this leaf exists for.
	Fingerprint string
}

// Commands lists every command the file declares, in one place.
//
// Shown means *all of them, together*. Revealing one at a time, as each event
// fires, would let a repository hide the fifth behind four boring ones — and
// the person approving would be answering a different question each time
// without knowing how many were left.
func (p Project) Commands() []string {
	var all []string
	for _, event := range Events() {
		all = append(all, p.Config.commandsFor(event)...)
	}
	return all
}

// LoadProject reads `.kolk/hooks.json` from a repository.
//
// It reads and does not run. A `.kolk/hooks.json` in a cloned repository is a
// shell command a stranger wrote, and cloning must not be enough to execute
// anything — so this returns what is there, and the caller shows it and asks
// before a single one of them runs.
//
// A malformed or empty file is not found rather than an error: it costs its
// hooks and not the session, and there is nothing to ask about.
func LoadProject(projectRoot string) (Project, bool) {
	path := filepath.Join(projectRoot, ".kolk", "hooks.json")
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return Project{}, false
	}
	var file struct {
		Hooks Config `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return Project{}, false
	}
	project := Project{Config: file.Hooks, Path: path}
	if len(project.Commands()) == 0 {
		return Project{}, false
	}
	sum := sha256.Sum256(raw)
	project.Fingerprint = hex.EncodeToString(sum[:])
	return project, true
}

// Merge combines the user's hooks with a project's.
//
// Both run, and the user's first. This is where hooks differ from markdown
// commands: a command is a *lookup*, so the nearer file wins a name and the
// other is never seen. A hook is an *action*, and there is no reason a
// project's formatter should silence the notification someone configured for
// themselves. Nearer does not mean instead-of when nothing is being named.
func Merge(user, project Config) Config {
	return Config{
		PostEdit:   append(append([]string{}, user.PostEdit...), project.PostEdit...),
		PostWrite:  append(append([]string{}, user.PostWrite...), project.PostWrite...),
		SessionEnd: append(append([]string{}, user.SessionEnd...), project.SessionEnd...),
	}
}
