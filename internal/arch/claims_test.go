package arch_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// staleClaims are sentences the code no longer honours. Each was true under
// the managed-sidecar contract plan 25 had before option E, and each is now a
// promise about behaviour that does not exist. History (CHECKPOINTS, the
// build log, plan 25's own record of what changed) may keep them; anything a
// user reads as current may not.
var staleClaims = []string{
	"never touches a host-owned Ollama",
	"never touches a host installation",
	"belongs to the host and is never used",
	"managed local runtime",
	"Kolk-owned runtime",
	"pins no verified local runtime",
	"/localia runtime install",
}

var claimExempt = []string{"CHECKPOINTS.md", "docs/build-log.md", "docs/plan/25-managed-local-models.md", "CHANGELOG.md", "claims_test.go"}

func TestNoManagedSidecarClaimsSurvive(t *testing.T) {
	root := repoRootDir(t)
	for _, glob := range []string{"README.md", "PLAN.md", "SECURITY.md", "site/*.html", "internal/**/*.go", "cmd/**/*.go", "docs/plan/*.md"} {
		matches, _ := filepath.Glob(filepath.Join(root, glob))
		if strings.Contains(glob, "**") {
			matches = nil
			_ = filepath.WalkDir(filepath.Join(root, strings.SplitN(glob, "/", 2)[0]), func(path string, d os.DirEntry, err error) error {
				if err == nil && !d.IsDir() && strings.HasSuffix(path, ".go") {
					matches = append(matches, path)
				}
				return nil
			})
		}
		for _, path := range matches {
			rel, _ := filepath.Rel(root, path)
			if exempt(rel) {
				continue
			}
			body, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			for _, claim := range staleClaims {
				if strings.Contains(string(body), claim) {
					t.Errorf("%s still says %q, which the code no longer does", rel, claim)
				}
			}
		}
	}
}

func exempt(rel string) bool {
	for _, e := range claimExempt {
		if strings.HasSuffix(rel, e) || rel == e {
			return true
		}
	}
	return false
}
