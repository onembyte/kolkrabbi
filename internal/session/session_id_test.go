package session

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/xid"
)

// The three path derivations (path, CooldownsFile, CkptDir) panic on an ID that
// never passed validateSessionID. That is only safe while every way to obtain a
// *Session validates first, so these tests hold both halves of the invariant:
// the behaviour of each constructor, and the fact that the list of constructors
// below is still the whole list.

// coveredConstructors are the package's ways of producing a *Session, each
// exercised for a validated ID by TestEveryConstructorValidatesTheID.
var coveredConstructors = map[string]bool{
	"New":  true,
	"Load": true,
	// List is not here: since OPTIMIZATION_PLAN.md O6 it returns headers, not
	// sessions, and a Meta derives no path. The two constructors built on it
	// still are, because they are what turns a header back into a *Session.
	"Latest":       true,
	"LatestForDir": true,
}

func TestTheListOfConstructorsIsComplete(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var found []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !fn.Name.IsExported() || fn.Type.Results == nil {
				continue
			}
			for _, result := range fn.Type.Results.List {
				if !returnsSession(result.Type) {
					continue
				}
				found = append(found, fn.Name.Name)
				break
			}
		}
	}
	if len(found) == 0 {
		t.Fatal("no constructors found at all; this test would pass vacuously")
	}
	sort.Strings(found)
	for _, name := range found {
		if !coveredConstructors[name] {
			t.Errorf("%s returns a *Session but is not in coveredConstructors: either it validates "+
				"the ID and belongs in the list with a case in TestEveryConstructorValidatesTheID, "+
				"or path()/CooldownsFile()/CkptDir() can now be reached with an unvalidated ID and "+
				"will panic", name)
		}
	}
	for name := range coveredConstructors {
		if !contains(found, name) {
			t.Errorf("coveredConstructors lists %s, which no longer returns a *Session", name)
		}
	}
}

// returnsSession reports whether a result type is *Session or []*Session.
func returnsSession(expr ast.Expr) bool {
	switch t := expr.(type) {
	case *ast.StarExpr:
		ident, ok := t.X.(*ast.Ident)
		return ok && ident.Name == "Session"
	case *ast.ArrayType:
		return returnsSession(t.Elt)
	}
	return false
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func TestEveryConstructorValidatesTheID(t *testing.T) {
	dir := t.TempDir()

	// A valid session on disk, plus a file whose contents carry an invalid ID.
	// The second is what a corrupt or hand-edited session directory looks like:
	// no constructor may hand it back, and none may crash on it.
	good := New(dir, "m")
	good.CWD = dir
	if err := good.Save(); err != nil {
		t.Fatal(err)
	}
	corruptID := xid.New(xid.Session)
	body, err := json.Marshal(map[string]any{"id": "../escape", "cwd": dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, corruptID+".json"), body, 0o600); err != nil {
		t.Fatal(err)
	}

	check := func(name string, s *Session) {
		t.Helper()
		if s == nil {
			t.Fatalf("%s returned no session", name)
		}
		if err := validateSessionID(s.ID); err != nil {
			t.Fatalf("%s returned a session whose ID does not validate: %v", name, err)
		}
		// The derivations must not panic, and must stay inside dir.
		for _, p := range []string{s.path(), s.CooldownsFile(), s.CkptDir()} {
			if filepath.Dir(p) != filepath.Clean(dir) {
				t.Fatalf("%s: derived path %q escapes %q", name, p, dir)
			}
		}
	}

	check("New", New(dir, "m"))

	loaded, err := Load(dir, good.ID)
	if err != nil {
		t.Fatal(err)
	}
	check("Load", loaded)

	all, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("List returned %d sessions; the corrupt one must be skipped, not returned", len(all))
	}
	// List hands back headers rather than sessions since OPTIMIZATION_PLAN.md
	// O6, so the derivations are Load's to check; what List still owes is that
	// no header it returns carries an id nothing validated.
	for _, m := range all {
		if err := validateSessionID(m.ID); err != nil {
			t.Fatalf("List returned a header whose ID does not validate: %v", err)
		}
	}

	latest, err := Latest(dir)
	if err != nil {
		t.Fatal(err)
	}
	check("Latest", latest)

	forDir, err := LatestForDir(dir, dir)
	if err != nil {
		t.Fatal(err)
	}
	check("LatestForDir", forDir)

	// And the corrupt file is an error, not a panic, when asked for by name.
	if _, err := Load(dir, corruptID); err == nil {
		t.Fatal("Load accepted a file carrying an invalid session id")
	}
}

// TestPathDerivationsPanicOnAnUnvalidatedID pins the remaining panic: it is
// reachable only by assembling a Session by hand, and it names the fix.
func TestPathDerivationsPanicOnAnUnvalidatedID(t *testing.T) {
	for name, derive := range map[string]func(*Session) string{
		"path":          (*Session).path,
		"CooldownsFile": (*Session).CooldownsFile,
		"CkptDir":       (*Session).CkptDir,
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("a hand-made Session derived a path from an unvalidated ID")
				}
				if msg, ok := r.(string); !ok || !strings.Contains(msg, "construct through New or Load") {
					t.Fatalf("panic %v does not tell the caller how to fix it", r)
				}
			}()
			derive(&Session{ID: "../escape", dir: t.TempDir()})
		})
	}
}
