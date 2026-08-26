// Package projectfiles lists the files in a project for completion.
//
// It is a convenience, and that shapes every decision here: it never returns an
// error, it caps what it returns, and when it cannot tell whether a file should
// be offered it leaves it out. A completion list is allowed to be incomplete. A
// session that fails to start because a directory could not be walked is not.
package projectfiles

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// skipDirs are never walked. They are the directories that make a completion
// list useless: a list whose first twenty entries are node_modules is not
// completion, and nobody has ever wanted to @-mention a build artefact.
var skipDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true,
	"node_modules": true, "vendor": true, "target": true,
	"dist": true, "build": true, ".venv": true, "venv": true,
	"__pycache__": true, ".mypy_cache": true, ".pytest_cache": true,
	".next": true, ".nuxt": true, ".gradle": true, ".idea": true,
}

// List walks a project and returns its files as slash-separated paths relative
// to root, sorted, at most limit of them.
func List(root string, limit int) []string {
	if limit <= 0 {
		limit = 500
	}
	ignore := readIgnore(root)

	var files []string
	_ = filepath.WalkDir(root, func(full string, entry fs.DirEntry, err error) error {
		if err != nil {
			return skipUnreadable(entry)
		}
		rel, relErr := filepath.Rel(root, full)
		if relErr != nil {
			// A path under the root that cannot be expressed relative to it is
			// not something to guess about; leave it out.
			return skipUnreadable(entry)
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)

		if entry.IsDir() {
			if skipDirs[entry.Name()] || ignore.matches(rel, true) {
				return fs.SkipDir
			}
			return nil
		}
		if ignore.matches(rel, false) {
			return nil
		}
		files = append(files, rel)
		return nil
	})

	slices.Sort(files)
	if len(files) > limit {
		files = files[:limit]
	}
	return files
}

// skipUnreadable turns a walk error into "carry on without it". An unreadable
// subtree must not be fatal: completion does not get to decide whether the
// session runs.
func skipUnreadable(entry fs.DirEntry) error {
	if entry != nil && entry.IsDir() {
		return fs.SkipDir
	}
	return nil
}

// ignoreList is the subset of .gitignore this understands.
//
// Deliberately a subset: exact names, directory names, and `*.ext` globs, with
// a leading slash anchoring to the project root. Negations (`!`) are skipped,
// which errs toward offering fewer files — the safe direction for a list whose
// only job is convenience. Anything more would be a gitignore implementation,
// and getting one subtly wrong is worse than having an obvious subset.
type ignoreList struct {
	patterns []ignorePattern
}

type ignorePattern struct {
	text     string
	anchored bool
	dirOnly  bool
}

func readIgnore(root string) ignoreList {
	body, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		return ignoreList{}
	}
	var list ignoreList
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		pattern := ignorePattern{text: line}
		if strings.HasSuffix(pattern.text, "/") {
			pattern.dirOnly = true
			pattern.text = strings.TrimSuffix(pattern.text, "/")
		}
		if strings.HasPrefix(pattern.text, "/") {
			pattern.anchored = true
			pattern.text = strings.TrimPrefix(pattern.text, "/")
		}
		if pattern.text == "" {
			continue
		}
		list.patterns = append(list.patterns, pattern)
	}
	return list
}

// matches reports whether a relative path is ignored.
func (l ignoreList) matches(rel string, isDir bool) bool {
	for _, pattern := range l.patterns {
		if pattern.dirOnly && !isDir {
			continue
		}
		if pattern.anchored {
			if matchPath(pattern.text, rel) {
				return true
			}
			continue
		}
		// Unanchored patterns match at any depth, which is what makes
		// `*.log` mean what people expect it to mean.
		if matchPath(pattern.text, rel) {
			return true
		}
		if matchPath(pattern.text, path.Base(rel)) {
			return true
		}
	}
	return false
}

func matchPath(pattern, name string) bool {
	if pattern == name {
		return true
	}
	ok, err := path.Match(pattern, name)
	return err == nil && ok
}
