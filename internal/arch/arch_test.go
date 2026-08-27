// arch_test.go enforces layers.go against the real tree.
//
// It parses every .go file in the module — including files excluded by build
// constraints, which is the point: a _windows.go file's imports must obey the
// rules on a Mac, where nobody ever compiles it.
package arch

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/onembyte/kolkrabbi"

// goFile is one parsed source file, with the package path it belongs to.
type goFile struct {
	pkg  string // package path relative to the module root, e.g. "internal/cli"
	rel  string // file path relative to the module root
	name string // base filename
	src  []byte
	ast  *ast.File
	fset *token.FileSet
}

func (f goFile) isTest() bool { return strings.HasSuffix(f.name, "_test.go") }

// root walks up from the test's working directory to the module root.
func root(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("could not find the module root (no go.mod above the test)")
	return ""
}

// parseTree reads every .go file in the root module. Nested modules are skipped:
// they have their own go.mod and are outside this module's dependency graph by
// construction, which is the whole reason they exist.
func parseTree(t *testing.T) []goFile {
	t.Helper()
	r := root(t)
	var out []goFile

	err := filepath.WalkDir(r, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(r, p)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)

		if d.IsDir() {
			switch {
			case rel == ".":
				return nil
			case strings.HasPrefix(d.Name(), "."), d.Name() == "testdata", d.Name() == "node_modules":
				return fs.SkipDir
			}
			// A nested module is a separate build; scripts/test.sh runs it.
			if _, err := os.Stat(filepath.Join(p, "go.mod")); err == nil {
				return fs.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, p, src, parser.ParseComments)
		if err != nil {
			return err
		}
		out = append(out, goFile{
			pkg:  path.Dir(rel),
			rel:  rel,
			name: d.Name(),
			src:  src,
			ast:  file,
			fset: fset,
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatal("parsed no Go files; the walk is broken, not the tree")
	}
	return out
}

func imports(f goFile) []string {
	var out []string
	for _, spec := range f.ast.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	return out
}

// isStdlib uses the go command's own rule: a standard-library path has no dot
// in its first element.
func isStdlib(p string) bool {
	first, _, _ := strings.Cut(p, "/")
	return !strings.Contains(first, ".")
}

// internalPkg turns an import of this module into a package path relative to
// the module root, or "" if the import is not ours.
func internalPkg(p string) string {
	if p == modulePath {
		return "."
	}
	if !strings.HasPrefix(p, modulePath+"/") {
		return ""
	}
	return strings.TrimPrefix(p, modulePath+"/")
}

// ── the rules ──────────────────────────────────────────────────────────────

// Every package must have a declared layer. A new package cannot be added
// without answering the question "which layer is this?".
func TestEveryPackageHasALayer(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range parseTree(t) {
		if seen[f.pkg] || commandPackages[f.pkg] {
			continue
		}
		seen[f.pkg] = true
		if _, ok := packageLayer[f.pkg]; !ok {
			t.Errorf("%s has no layer in layers.go — add it to packageLayer (or commandPackages)", f.pkg)
		}
	}
}

// The table must not name packages that do not exist, or it stops describing
// the tree and starts describing a memory of it.
func TestLayerTableHasNoGhosts(t *testing.T) {
	r := root(t)
	for pkg := range packageLayer {
		if _, err := os.Stat(filepath.Join(r, filepath.FromSlash(pkg))); err != nil {
			t.Errorf("layers.go names %s, which does not exist", pkg)
		}
	}
}

func TestImportDirection(t *testing.T) {
	for _, f := range parseTree(t) {
		from, ok := packageLayer[f.pkg]
		if !ok {
			continue // reported by TestEveryPackageHasALayer
		}
		// A package's own tests are its wiring: they construct adapters and
		// fakes on purpose, and nothing a consumer compiles depends on them.
		// The contract layer is the exception — its conformance suite is meant
		// to be the language-neutral check, so it may not reach inward either.
		if f.isTest() && from != L1Contract {
			continue
		}
		for _, imp := range imports(f) {
			target := internalPkg(imp)
			if target == "" {
				continue
			}
			to, ok := packageLayer[target]
			if !ok {
				t.Errorf("%s imports %s, which has no layer", f.rel, target)
				continue
			}
			if from == to || allowed(from, to) || isKnown(f.pkg, target) {
				continue
			}
			t.Errorf("%s (%s) imports %s (%s): %s may not import %s\n\tsee docs/plan/02-architecture.md §5",
				f.rel, from, target, to, from, to)
		}
	}
}

func allowed(from, to Layer) bool {
	for _, l := range mayImport[from] {
		if l == to {
			return true
		}
	}
	return false
}

// The engine declares ports; surfaces inject implementations. Stated as its own
// test because it is the rule that makes the daemon, the desktop shell and the
// test fakes all possible, and a failure should say so rather than read as a
// generic layering complaint.
func TestEngineImportsNoAdapter(t *testing.T) {
	for _, f := range parseTree(t) {
		if packageLayer[f.pkg] != L4Engine || f.isTest() {
			continue
		}
		for _, imp := range imports(f) {
			target := internalPkg(imp)
			if target != "" && packageLayer[target] == L5Adapter && !isKnown(f.pkg, target) {
				t.Errorf("%s imports the adapter %s — the engine must declare a port and let a surface inject it",
					f.rel, target)
			}
		}
	}
}

func TestThirdPartyAllowList(t *testing.T) {
	for _, f := range parseTree(t) {
		for _, imp := range imports(f) {
			if isStdlib(imp) || internalPkg(imp) != "" {
				continue
			}
			if !thirdPartyAllowed(f.pkg, imp) {
				t.Errorf("%s imports %s, which is not on %s's allow-list in layers.go",
					f.rel, imp, f.pkg)
			}
		}
	}
}

func thirdPartyAllowed(pkg, imp string) bool {
	for _, prefix := range thirdParty[pkg] {
		if imp == strings.TrimSuffix(prefix, "/") || strings.HasPrefix(imp, prefix) ||
			strings.HasPrefix(imp, prefix+"/") {
			return true
		}
	}
	return false
}

// One package owns each piece of the OS. Enforced on non-test files only: a
// test may set up whatever it needs to test the owner.
func TestOSAccessHasOneOwner(t *testing.T) {
	files := parseTree(t)

	for _, f := range files {
		if f.isTest() || commandPackages[f.pkg] {
			continue
		}
		for _, imp := range imports(f) {
			owner, guarded := osOwner[imp]
			if !guarded || f.pkg == owner {
				continue
			}
			if isKnown(f.pkg, imp) {
				continue
			}
			t.Errorf("%s imports %s; only %s may (see layers.go osOwner)", f.rel, imp, owner)
		}
	}

	for _, f := range files {
		if f.isTest() {
			continue
		}
		for _, sel := range selectors(f, "os") {
			rule := "os." + sel.Sel.Name
			owner, guarded := osOwner[rule]
			if !guarded || f.pkg == owner || isKnown(f.pkg, rule) {
				continue
			}
			t.Errorf("%s calls %s at %s; only %s may (see layers.go osOwner)",
				f.rel, rule, f.fset.Position(sel.Pos()), owner)
		}
	}
}

func TestPackagesHaveNoForbiddenImports(t *testing.T) {
	for _, f := range parseTree(t) {
		if f.isTest() {
			continue
		}
		for _, imp := range imports(f) {
			for _, forbidden := range forbiddenImports[f.pkg] {
				if imp == forbidden {
					t.Errorf("%s imports %s, which %s is forbidden to access", f.rel, imp, f.pkg)
				}
			}
		}
	}
}

func TestStdlibOnlyPackagesStayIndependent(t *testing.T) {
	for _, f := range parseTree(t) {
		if f.isTest() || !stdlibOnlyPackages[f.pkg] {
			continue
		}
		for _, imp := range imports(f) {
			if !isStdlib(imp) {
				t.Errorf("%s imports %s; %s is a standard-library-only package", f.rel, imp, f.pkg)
			}
		}
	}
}

// selectors finds every pkg.Name expression in a file for the given package
// identifier. Working on the AST rather than the file text means a rule name
// appearing in a comment or a string is not a false positive.
func selectors(f goFile, pkg string) []*ast.SelectorExpr {
	var out []*ast.SelectorExpr
	ast.Inspect(f.ast, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == pkg {
			out = append(out, sel)
		}
		return true
	})
	return out
}

// Branching on runtime.GOOS is the thing build tags exist to replace: it
// compiles everywhere, so the compiler never tells you the other branch is
// wrong. Reading GOOS as a value — to tell a model which OS it is on, say — is
// fine, so this bans the comparison rather than the identifier.
func TestNoRuntimeGOOSBranching(t *testing.T) {
	for _, f := range parseTree(t) {
		// Tests may compare GOOS: asserting on it, and skipping a case that
		// cannot run on this platform, are both legitimate and neither ships.
		if f.isTest() {
			continue
		}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.BinaryExpr:
				if n.Op != token.EQL && n.Op != token.NEQ {
					return true
				}
				if isGOOS(n.X) || isGOOS(n.Y) {
					t.Errorf("%s compares runtime.GOOS at %s — use a build-tagged file instead\n\tsee docs/plan/02-architecture.md §8",
						f.rel, f.fset.Position(n.Pos()))
				}
			case *ast.SwitchStmt:
				if n.Tag != nil && isGOOS(n.Tag) {
					t.Errorf("%s switches on runtime.GOOS at %s — use a build-tagged file instead",
						f.rel, f.fset.Position(n.Pos()))
				}
			}
			return true
		})
	}
}

func isGOOS(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "GOOS" {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "runtime"
}

// Cancellation must reach every blocking call, so a context is passed in rather
// than invented. cmd/ may invent one — it is the root of the tree — and tests
// may, because they are the root of their own.
func TestNoInventedContexts(t *testing.T) {
	for _, f := range parseTree(t) {
		if f.isTest() || commandPackages[f.pkg] {
			continue
		}
		for _, sel := range selectors(f, "context") {
			if sel.Sel.Name != "Background" && sel.Sel.Name != "TODO" {
				continue
			}
			t.Errorf("%s calls context.%s at %s — take a ctx parameter instead\n\tsee docs/plan/02-architecture.md §10",
				f.rel, sel.Sel.Name, f.fset.Position(sel.Pos()))
		}
	}
}

// A filename suffix the go command does not interpret is a trap: shell_unix.go
// with no //go:build line compiles on Windows too, silently. Suffixes it DOES
// interpret are included anyway, because "every OS-divergent file states its
// constraint" is a rule you can follow without remembering which is which.
func TestOSFilesDeclareTheirBuildConstraint(t *testing.T) {
	for _, f := range parseTree(t) {
		suffix := ""
		for _, s := range tagRequiredSuffixes {
			if strings.HasSuffix(f.name, s) || strings.HasSuffix(f.name, strings.TrimSuffix(s, ".go")+"_test.go") {
				suffix = s
				break
			}
		}
		if suffix == "" {
			continue
		}
		if !hasBuildConstraint(f) {
			t.Errorf("%s has no //go:build line. %q is not a GOOS the go command recognises on its own, so this file compiles on every platform\n\tsee docs/plan/02-architecture.md §8",
				f.rel, strings.TrimSuffix(strings.TrimPrefix(suffix, "_"), ".go"))
		}
	}
}

// "The engine touches no OS" has to be a build failure, not a code-review
// convention. A GOOS-suffixed file anywhere above L0 means the divergence
// escaped the platform layer, and every constrained target on the roadmap —
// the daemon, the iPad, a gomobile build — depends on that not happening.
func TestOnlyThePlatformLayerHasOSFiles(t *testing.T) {
	for _, f := range parseTree(t) {
		layer, ok := packageLayer[f.pkg]
		if !ok || layer == L0Platform || layer == LTestKit || f.isTest() {
			continue
		}
		for _, suffix := range tagRequiredSuffixes {
			if strings.HasSuffix(f.name, suffix) {
				t.Errorf("%s is OS-divergent but sits in %s; put the divergence behind an interface in the platform layer\n\tsee docs/plan/02-architecture.md §8",
					f.rel, layer)
			}
		}
	}
}

func hasBuildConstraint(f goFile) bool {
	for _, group := range f.ast.Comments {
		for _, c := range group.List {
			if !strings.HasPrefix(c.Text, "//go:build") {
				continue
			}
			// Must precede the package clause to be a build constraint at all.
			if c.Pos() < f.ast.Package {
				return true
			}
		}
	}
	return false
}

// A credential must only ever become a header inside internal/secret, on a
// request clone that never escapes. This is the rule behind that: any other
// package setting an auth header is putting the key on an object a caller
// holds and may print.
func TestOnlySecretBuildsAuthHeaders(t *testing.T) {
	for _, f := range parseTree(t) {
		if f.pkg == authHeaderOwner || f.isTest() {
			continue
		}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (sel.Sel.Name != "Set" && sel.Sel.Name != "Add") {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			name, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			for _, h := range authHeaders {
				if strings.EqualFold(name, h) {
					t.Errorf("%s sets the %s header at %s; only %s may — a header is a plain map and cannot redact itself\n\tuse secret.AuthTransport",
						f.rel, h, f.fset.Position(call.Pos()), authHeaderOwner)
				}
			}
			return true
		})
	}
}

func TestNoBannedNames(t *testing.T) {
	seenPkg := map[string]bool{}
	for _, f := range parseTree(t) {
		for _, banned := range bannedFilenames {
			if f.name == banned {
				t.Errorf("%s: a file named %q describes nothing — name it for the concept it holds", f.rel, banned)
			}
		}
		name := f.ast.Name.Name
		if seenPkg[name] {
			continue
		}
		seenPkg[name] = true
		for _, banned := range bannedPackageNames {
			if name == banned {
				t.Errorf("%s: package %q is banned — a package is named for a role", f.rel, banned)
			}
		}
	}
	r := root(t)
	if _, err := os.Stat(filepath.Join(r, "pkg")); err == nil {
		t.Error("a pkg/ directory exists; this repo does not use one")
	}
}

// `go install <module>/cmd/x@version` hard-fails on any module whose go.mod
// carries a replace directive, which is exactly how kolk is meant to be
// installed. gopls strips its own replace at every release tag for this reason.
func TestRootGoModHasNoReplace(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(root(t), "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "replace ") ||
			strings.TrimSpace(line) == "replace (" {
			t.Errorf("go.mod:%d has a replace directive; `go install …@latest` refuses a module with one", i+1)
		}
	}
}

// The ratchet's second half: a known violation that has been fixed must be
// removed from the list. Without this the list quietly becomes a permanent
// exemption for rules nobody breaks any more.
func TestKnownViolationsAreStillReal(t *testing.T) {
	files := parseTree(t)

	stillViolates := func(v violation) bool {
		for _, f := range files {
			if f.pkg != v.Pkg || f.isTest() {
				continue
			}
			for _, imp := range imports(f) {
				if imp == v.Rule || internalPkg(imp) == v.Rule {
					return true
				}
			}
			if pkg, name, ok := strings.Cut(v.Rule, "."); ok {
				for _, sel := range selectors(f, pkg) {
					if sel.Sel.Name == name {
						return true
					}
				}
			}
		}
		return false
	}

	for _, v := range knownViolations {
		if !stillViolates(v) {
			t.Errorf("knownViolations lists %s / %s, but it no longer violates the rule — delete the entry (%s)",
				v.Pkg, v.Rule, v.Until)
		}
	}
}

// Every entry must name the step that removes it, so the list reads as debt
// with a due date rather than a list of things we gave up on.
func TestKnownViolationsNameTheirFix(t *testing.T) {
	for _, v := range knownViolations {
		if v.Pkg == "" || v.Rule == "" || v.Until == "" {
			t.Errorf("knownViolations entry %+v is incomplete; every entry names the step that removes it", v)
		}
	}
}

// TestTheThirdPartyAllowListDoesNotRot is the reverse direction of
// TestThirdPartyAllowList, and it exists for the same reason the dead-export
// allowlist has a rot test: an allowance nobody uses is a decision nobody
// re-reads.
//
// The specific case that prompted it: `internal/dash` was allowed
// modernc.org/sqlite on the strength of item 2's claim that it is "the one
// heavy dependency" of the dashboard. The dashboard then shipped rendering
// entirely on the server with no database at all, and the allowance stayed —
// quietly pre-approving a 400-file dependency for a package that had decided it
// did not want one.
//
// A budget that pre-approves what nobody asked for is not a budget.
func TestTheThirdPartyAllowListDoesNotRot(t *testing.T) {
	used := map[string]bool{}
	for _, f := range parseTree(t) {
		for _, imp := range imports(f) {
			if isStdlib(imp) || internalPkg(imp) != "" {
				continue
			}
			for _, prefix := range thirdParty[f.pkg] {
				if imp == strings.TrimSuffix(prefix, "/") || strings.HasPrefix(imp, prefix+"/") {
					used[f.pkg+" "+prefix] = true
				}
			}
		}
	}

	for pkg, allowances := range thirdParty {
		for _, allowance := range allowances {
			if !used[pkg+" "+allowance] {
				t.Errorf("layers.go allows %s to import %s, and nothing does — "+
					"wire it or drop the allowance", pkg, allowance)
			}
		}
	}
}
