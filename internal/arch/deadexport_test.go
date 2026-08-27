package arch_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/arch"
)

// TestNoExportedSymbolIsUsedOnlyWhereItLives finds code that is exported for
// nobody.
//
// `internal/` means nothing outside this module can ever refer to these names,
// so a symbol no other directory mentions is either dead or needlessly
// exported. golangci-lint's `unused` will not say so: it treats an exported
// identifier as a package's public API, which is right for a library and wrong
// here. Proved by experiment on 2026-08-27 — an obviously uncalled exported
// function produced `0 issues` — and that blind spot is how FileGateDetector
// and ChapterVerifier survived with green tests for months.
//
// The rule is deliberately "nothing but tests uses it", not "only its own
// package uses it". A package exporting an API its own tests exercise is a
// style choice — internal/tui alone has 36 such symbols and every one is
// deliberate. Code that only tests touch is a different thing: it looks
// maintained, it costs review attention, and it is what this rule is for.
//
// A declaration contributes exactly one identifier occurrence, so a symbol with
// no more than that in non-test code is reachable only from tests.
func TestNoExportedSymbolIsUsedOnlyByTests(t *testing.T) {
	declared, nonTestUses := scanTree(t)

	var orphans []string
	for name, dir := range declared {
		if _, ok := arch.DeadExportAllowlist[name]; ok {
			continue
		}
		if nonTestUses[name] <= 1 {
			orphans = append(orphans, dir+"."+name)
		}
	}
	sort.Strings(orphans)

	for _, orphan := range orphans {
		t.Errorf("%s is reachable only from tests — wire it, delete it, or add it to arch.DeadExportAllowlist with the reason", orphan)
	}
}

// TestTheDeadExportAllowlistDoesNotRot keeps the exemptions honest.
//
// An exemption for a symbol that has since found a caller, or that no longer
// exists, is a claim nobody re-reads — the same rot that let a hardcoded verb
// list outlive three of its thirteen names.
func TestTheDeadExportAllowlistDoesNotRot(t *testing.T) {
	declared, nonTestUses := scanTree(t)
	for name, reason := range arch.DeadExportAllowlist {
		if _, ok := declared[name]; !ok {
			t.Errorf("DeadExportAllowlist exempts %q, which is not an exported symbol in internal/", name)
			continue
		}
		if reason == "" {
			t.Errorf("%q is exempt with no reason given", name)
		}
		if nonTestUses[name] > 1 {
			t.Errorf("%q has non-test callers now and no longer needs an exemption", name)
		}
	}
}

// scanTree returns where each exported symbol is declared, and how many times
// each name appears as an identifier in non-test code.
func scanTree(t *testing.T) (declared map[string]string, nonTestUses map[string]int) {
	t.Helper()
	declared = map[string]string{}
	nonTestUses = map[string]int{}
	root := repoRootDir(t)

	for _, base := range []string{"internal", "cmd", "protocol"} {
		walk(t, filepath.Join(root, base), func(path string, file *ast.File) {
			dir := filepath.ToSlash(strings.TrimPrefix(filepath.Dir(path), root+string(filepath.Separator)))
			isTest := strings.HasSuffix(path, "_test.go")

			if !isTest && strings.HasPrefix(dir, "internal/") {
				for _, name := range exportedDecls(file) {
					// First declaration wins; a duplicate name across packages
					// is rare and would only weaken the check, never break it.
					if _, seen := declared[name]; !seen {
						declared[name] = dir
					}
				}
			}
			if isTest {
				return
			}
			ast.Inspect(file, func(n ast.Node) bool {
				// A method receiver is not a use of its type: `func (T) M()`
				// is part of T's own definition. Counting it hid the case this
				// rule was written for — FileGateDetector, whose only mention
				// outside tests was the receiver of its one method.
				if fn, ok := n.(*ast.FuncDecl); ok && fn.Recv != nil {
					ast.Inspect(fn.Type, countIdents(nonTestUses))
					if fn.Body != nil {
						ast.Inspect(fn.Body, countIdents(nonTestUses))
					}
					nonTestUses[fn.Name.Name]++
					return false
				}
				if ident, ok := n.(*ast.Ident); ok {
					nonTestUses[ident.Name]++
				}
				return true
			})
		})
	}
	return declared, nonTestUses
}

func skipEntry(entry os.DirEntry) error {
	if entry != nil && entry.IsDir() {
		return filepath.SkipDir
	}
	return nil
}

func countIdents(into map[string]int) func(ast.Node) bool {
	return func(n ast.Node) bool {
		if ident, ok := n.(*ast.Ident); ok {
			into[ident.Name]++
		}
		return true
	}
}

func exportedDecls(file *ast.File) []string {
	var names []string
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil && d.Name.IsExported() {
				names = append(names, d.Name.Name)
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if s.Name.IsExported() {
						names = append(names, s.Name.Name)
					}
				case *ast.ValueSpec:
					for _, ident := range s.Names {
						if ident.IsExported() {
							names = append(names, ident.Name)
						}
					}
				}
			}
		}
	}
	return names
}

func walk(t *testing.T, dir string, fn func(string, *ast.File)) {
	t.Helper()
	fset := token.NewFileSet()
	_ = filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			// An unreadable entry cannot hide a dead export from us: the
			// symbol would simply never be declared. Skip it rather than
			// failing an architecture test over a filesystem hiccup.
			return skipEntry(entry)
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		parsed, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			t.Fatalf("parsing %s: %v", path, perr)
		}
		fn(path, parsed)
		return nil
	})
}

func repoRootDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(filepath.Dir(wd))
}
