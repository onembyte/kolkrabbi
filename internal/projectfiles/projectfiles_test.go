package projectfiles

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestFilesAreListedWithSlashPathsAndSorted(t *testing.T) {
	root := tree(t, map[string]string{
		"main.go":         "",
		"internal/a/b.go": "",
		"README.md":       "",
	})

	got := List(root, 100)

	want := []string{"README.md", "internal/a/b.go", "main.go"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestTheGitDirectoryIsNeverListed(t *testing.T) {
	root := tree(t, map[string]string{
		"main.go":            "",
		".git/config":        "",
		".git/objects/ab/cd": "",
	})

	for _, path := range List(root, 100) {
		if strings.HasPrefix(path, ".git/") {
			t.Fatalf("listed %q", path)
		}
	}
}

func TestHeavyDirectoriesAreSkippedWithoutBeingConfigured(t *testing.T) {
	// A completion list whose first twenty entries are node_modules is not
	// completion.
	root := tree(t, map[string]string{
		"app.js":                     "",
		"node_modules/left-pad/i.js": "",
		"vendor/x/y.go":              "",
		"target/debug/thing":         "",
	})

	got := List(root, 100)
	if !slices.Equal(got, []string{"app.js"}) {
		t.Fatalf("got %v, want only the project's own file", got)
	}
}

func TestGitignoreEntriesAreHonoured(t *testing.T) {
	root := tree(t, map[string]string{
		".gitignore":    "secrets.env\n*.log\nbuild/\n\n# a comment\n",
		"main.go":       "",
		"secrets.env":   "",
		"debug.log":     "",
		"build/out.bin": "",
		"src/other.log": "",
	})

	got := List(root, 100)

	if !slices.Contains(got, "main.go") {
		t.Fatalf("got %v, lost a real file", got)
	}
	for _, unwanted := range []string{"secrets.env", "debug.log", "build/out.bin", "src/other.log"} {
		if slices.Contains(got, unwanted) {
			t.Fatalf("listed ignored path %q in %v", unwanted, got)
		}
	}
}

func TestAnAnchoredPatternOnlyMatchesAtTheRoot(t *testing.T) {
	root := tree(t, map[string]string{
		".gitignore":      "/config.json\n",
		"config.json":     "",
		"sub/config.json": "",
	})

	got := List(root, 100)
	if slices.Contains(got, "config.json") {
		t.Fatalf("the anchored pattern did not match at the root: %v", got)
	}
	if !slices.Contains(got, "sub/config.json") {
		t.Fatalf("the anchored pattern matched too deep: %v", got)
	}
}

func TestTheListIsCapped(t *testing.T) {
	files := map[string]string{}
	for i := range 50 {
		files["f"+string(rune('a'+i%26))+string(rune('a'+i/26))+".txt"] = ""
	}
	root := tree(t, files)

	if got := List(root, 10); len(got) != 10 {
		t.Fatalf("got %d files, want the cap", len(got))
	}
}

func TestAnUnreadableRootIsNotAnError(t *testing.T) {
	// Completion is a convenience. It must never be the reason a session
	// cannot start.
	if got := List(filepath.Join(t.TempDir(), "nope"), 10); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}
