package cli

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onembyte/kolkrabbi/internal/config"
	"github.com/onembyte/kolkrabbi/internal/local"
)

// The CPU choice (local.gpu_mode cpu) is read when an installer is built, so
// a change applies at once, and only an explicit cpu counts: auto, gpu or
// nothing leave the automatic choice in charge.
func TestTheInstallerCarriesTheCPUChoice(t *testing.T) {
	a, _, _ := newTestApp(t, "")
	for _, c := range []struct {
		set  string
		want bool
	}{{"", false}, {"cpu", true}, {"auto", false}, {"gpu", false}, {"cpu", true}} {
		if c.set != "" {
			if err := a.runConfig(context.Background(), []string{"set", "local.gpu_mode", c.set}); err != nil {
				t.Fatal(err)
			}
		}
		if got := a.localInstaller("/tmp/kolk-runtimes", nil).CPUOnly; got != c.want {
			t.Errorf("gpu_mode %q: CPUOnly = %v, want %v", c.set, got, c.want)
		}
		// Setup always reports progress; the choice holds there too.
		if got := a.localInstaller("/tmp/kolk-runtimes", func(local.RuntimeProgress) {}).CPUOnly; got != c.want {
			t.Errorf("gpu_mode %q with progress: CPUOnly = %v, want %v", c.set, got, c.want)
		}
	}
	// Written by hand as "CPU": read as the planner reads it (fit.go folds case).
	d, err := a.locate()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(d.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Local.GPUMode = " CPU "
	if err := config.Save(d.ConfigFile(), cfg); err != nil {
		t.Fatal(err)
	}
	if !a.localInstaller("/tmp/kolk-runtimes", nil).CPUOnly {
		t.Error("a hand-written \" CPU \" was not read as CPU chosen")
	}
}

// Every installer the CLI builds comes from localInstaller, so discovery,
// setup and housekeeping all see the same choice. Any reference to the type
// elsewhere (a literal, new, a var, a conversion, an alias under any import
// name) is a site that could miss it. Read as syntax, so a comment is not a
// site.
func TestEveryRuntimeInstallerIsBuiltInOnePlace(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	sites := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			if fn, ok := node.(*ast.FuncDecl); ok && fn.Name.Name == "localInstaller" && fn.Recv != nil {
				return false // the one place allowed
			}
			if sel, ok := node.(*ast.SelectorExpr); ok && sel.Sel.Name == "RuntimeInstaller" {
				sites++
				t.Errorf("%s: RuntimeInstaller used outside localInstaller", fset.Position(sel.Pos()))
			}
			return true
		})
	}
	if sites != 0 {
		t.Errorf("%d RuntimeInstaller references outside localInstaller", sites)
	}
}
