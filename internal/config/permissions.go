package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/onembyte/kolkrabbi/internal/atomicfile"
)

// Permissions is the user's standing answers to questions Kolkrabbi would
// otherwise ask every time.
//
// It lives beside config.json rather than inside it because it is edited by a
// different act: config is a preference someone sets once, this is a list that
// grows every time someone says "yes, and stop asking". Keeping them apart
// means a corrupt rule list cannot cost someone their model choice.
//
// Rules are stored as the lines the user wrote, not as parsed structures. The
// file is meant to be opened and read; a JSON object per rule would be a format
// nobody can review, which for a permission list is the whole point of having
// it on disk.
type Permissions struct {
	// Always applies in every project.
	Always []string `json:"always,omitempty"`
	// Projects is keyed by project root: a rule someone accepted for one
	// checkout has no business applying to another.
	Projects map[string][]string `json:"projects,omitempty"`
}

// PermissionsFile is where the rules live, given the config directory.
func PermissionsFile(configDir string) string {
	return filepath.Join(configDir, "permissions.json")
}

// LoadPermissions reads the rule file. A missing file is not an error: having
// no rules is the normal state, not a broken one.
func LoadPermissions(file string) (*Permissions, error) {
	b, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return &Permissions{}, nil
	}
	if err != nil {
		return nil, err
	}
	var stored Permissions
	if err := json.Unmarshal(b, &stored); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", file, err)
	}
	return &stored, nil
}

// SavePermissions writes the rule file, creating its directory if needed.
func SavePermissions(file string, stored *Permissions) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}
	// 0600: a rule list an attacker can append to is a rule list that can
	// grant them anything.
	return atomicfile.Write(file, append(b, '\n'), 0o600)
}

// For returns the rules that apply in one project, global ones first.
//
// The order is the semantics: downstream the last matching rule wins, so a
// project's own list is what refines the global one and never the reverse.
func (p *Permissions) For(root string) []string {
	rules := make([]string, 0, len(p.Always)+len(p.Projects[root]))
	rules = append(rules, p.Always...)
	if root != "" {
		rules = append(rules, p.Projects[root]...)
	}
	return rules
}

// Add records a rule. An empty root means every project.
func (p *Permissions) Add(rule, root string) {
	if root == "" {
		if !containsString(p.Always, rule) {
			p.Always = append(p.Always, rule)
		}
		return
	}
	if p.Projects == nil {
		p.Projects = map[string][]string{}
	}
	if !containsString(p.Projects[root], rule) {
		p.Projects[root] = append(p.Projects[root], rule)
	}
}

// Remove takes a rule out of one scope, reporting whether it was there. A rule
// someone cannot find and delete is one they will work around instead.
func (p *Permissions) Remove(rule, root string) bool {
	if root == "" {
		var kept []string
		removed := false
		for _, existing := range p.Always {
			if existing == rule {
				removed = true
				continue
			}
			kept = append(kept, existing)
		}
		p.Always = kept
		return removed
	}
	var kept []string
	removed := false
	for _, existing := range p.Projects[root] {
		if existing == rule {
			removed = true
			continue
		}
		kept = append(kept, existing)
	}
	if len(kept) == 0 {
		delete(p.Projects, root)
	} else {
		p.Projects[root] = kept
	}
	return removed
}

func containsString(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}
